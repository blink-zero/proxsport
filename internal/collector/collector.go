package collector

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/blink-zero/proxsport/internal/config"
	"github.com/blink-zero/proxsport/internal/proxmox"
)

// Collector orchestrates polling Proxmox and exposing metrics. It implements
// prometheus.Collector but does NOT scrape Proxmox during Collect() — that
// would couple Proxmox API load to Prometheus scrape frequency and is fragile
// under transient Proxmox slowness. Instead a background poller refreshes
// metrics on a fixed cadence and Collect() reads the cache.
type Collector struct {
	client *proxmox.Client
	cfg    *config.Config

	mu              sync.RWMutex
	resources       []proxmox.Resource
	clusterStatus   []proxmox.ClusterStatusEntry
	lastPollSuccess time.Time
	lastPollError   error

	// Metrics (registered on the prometheus.Registry by Register())
	up               *prometheus.Desc
	scrapeDuration   *prometheus.Desc
	scrapeError      *prometheus.Desc
	clusterQuorate   *prometheus.Desc
	clusterNodes     *prometheus.Desc

	nodeUp              *prometheus.Desc
	nodeCPUUsage        *prometheus.Desc
	nodeCPUCores        *prometheus.Desc
	nodeMemoryUsed      *prometheus.Desc
	nodeMemoryTotal     *prometheus.Desc
	nodeDiskUsed        *prometheus.Desc
	nodeDiskTotal       *prometheus.Desc
	nodeUptime          *prometheus.Desc

	guestStatus         *prometheus.Desc
	guestCPUUsage       *prometheus.Desc
	guestCPUCores       *prometheus.Desc
	guestMemoryUsed     *prometheus.Desc
	guestMemoryTotal    *prometheus.Desc
	guestDiskUsed       *prometheus.Desc
	guestDiskTotal      *prometheus.Desc
	guestUptime         *prometheus.Desc
	guestNetRX          *prometheus.Desc
	guestNetTX          *prometheus.Desc
	guestDiskRead       *prometheus.Desc
	guestDiskWrite      *prometheus.Desc

	storageUsed   *prometheus.Desc
	storageTotal  *prometheus.Desc
	storageActive *prometheus.Desc
}

// New constructs a Collector for the given client + config.
func New(client *proxmox.Client, cfg *config.Config) *Collector {
	c := &Collector{client: client, cfg: cfg}
	c.registerDescriptors()
	return c
}

const namespace = "proxsport"

func (c *Collector) registerDescriptors() {
	c.up = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "up"),
		"1 if the last Proxmox API poll succeeded, 0 if it failed.",
		nil, nil,
	)
	c.scrapeDuration = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "scrape", "duration_seconds"),
		"Wall-clock duration of the most recent successful poll cycle, in seconds.",
		nil, nil,
	)
	c.scrapeError = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "scrape", "errors_total"),
		"Cumulative count of failed poll cycles since proxsport started.",
		nil, nil,
	)
	c.clusterQuorate = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "cluster", "quorate"),
		"1 if the cluster is quorate, 0 otherwise.",
		[]string{"cluster"}, nil,
	)
	c.clusterNodes = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "cluster", "nodes_total"),
		"Total number of nodes in the cluster.",
		[]string{"cluster"}, nil,
	)

	nodeLabels := []string{"node"}
	c.nodeUp = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "node", "up"),
		"1 if the Proxmox node is online, 0 otherwise.",
		nodeLabels, nil,
	)
	c.nodeCPUUsage = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "node", "cpu_usage_ratio"),
		"Current CPU usage as a ratio of total available CPU on the node, 0-1.",
		nodeLabels, nil,
	)
	c.nodeCPUCores = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "node", "cpu_cores"),
		"Number of CPU cores reported by the node.",
		nodeLabels, nil,
	)
	c.nodeMemoryUsed = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "node", "memory_used_bytes"),
		"Memory currently in use on the node, in bytes.",
		nodeLabels, nil,
	)
	c.nodeMemoryTotal = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "node", "memory_total_bytes"),
		"Total memory installed on the node, in bytes.",
		nodeLabels, nil,
	)
	c.nodeDiskUsed = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "node", "rootdisk_used_bytes"),
		"Root-disk space currently used on the node, in bytes.",
		nodeLabels, nil,
	)
	c.nodeDiskTotal = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "node", "rootdisk_total_bytes"),
		"Total root-disk capacity on the node, in bytes.",
		nodeLabels, nil,
	)
	c.nodeUptime = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "node", "uptime_seconds"),
		"Time the node has been up, in seconds.",
		nodeLabels, nil,
	)

	guestLabels := []string{"vmid", "name", "node", "type", "tags"}
	c.guestStatus = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "guest", "status"),
		"1 if the guest is running, 0 otherwise.",
		guestLabels, nil,
	)
	c.guestCPUUsage = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "guest", "cpu_usage_ratio"),
		"Current CPU usage as a ratio of allocated CPU, 0-1.",
		guestLabels, nil,
	)
	c.guestCPUCores = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "guest", "cpu_cores"),
		"Allocated vCPU cores for the guest.",
		guestLabels, nil,
	)
	c.guestMemoryUsed = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "guest", "memory_used_bytes"),
		"Memory currently in use by the guest, in bytes (only meaningful while running).",
		guestLabels, nil,
	)
	c.guestMemoryTotal = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "guest", "memory_total_bytes"),
		"Memory allocated to the guest, in bytes.",
		guestLabels, nil,
	)
	c.guestDiskUsed = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "guest", "disk_used_bytes"),
		"Disk used by the guest, in bytes (LXC: in-FS used; QEMU: 0 if not reported by agent).",
		guestLabels, nil,
	)
	c.guestDiskTotal = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "guest", "disk_total_bytes"),
		"Total disk allocated to the guest, in bytes.",
		guestLabels, nil,
	)
	c.guestUptime = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "guest", "uptime_seconds"),
		"Time the guest has been up, in seconds (0 if stopped).",
		guestLabels, nil,
	)
	c.guestNetRX = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "guest", "network_receive_bytes_total"),
		"Total bytes received by the guest since boot (counter, resets on reboot).",
		guestLabels, nil,
	)
	c.guestNetTX = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "guest", "network_transmit_bytes_total"),
		"Total bytes transmitted by the guest since boot (counter, resets on reboot).",
		guestLabels, nil,
	)
	c.guestDiskRead = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "guest", "disk_read_bytes_total"),
		"Total bytes read from disk by the guest since boot (counter).",
		guestLabels, nil,
	)
	c.guestDiskWrite = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "guest", "disk_write_bytes_total"),
		"Total bytes written to disk by the guest since boot (counter).",
		guestLabels, nil,
	)

	storageLabels := []string{"node", "storage", "plugin"}
	c.storageUsed = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "storage", "used_bytes"),
		"Used capacity of the storage pool, in bytes.",
		storageLabels, nil,
	)
	c.storageTotal = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "storage", "total_bytes"),
		"Total capacity of the storage pool, in bytes.",
		storageLabels, nil,
	)
	c.storageActive = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "storage", "active"),
		"1 if the storage pool is active and reachable, 0 otherwise.",
		storageLabels, nil,
	)
}

// Run starts the background polling loop. Blocks until ctx is cancelled.
func (c *Collector) Run(ctx context.Context) {
	c.poll(ctx) // initial poll so /metrics has data immediately
	tick := time.NewTicker(c.cfg.PollInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			c.poll(ctx)
		}
	}
}

var pollErrors uint64

func (c *Collector) poll(ctx context.Context) {
	start := time.Now()
	pollCtx, cancel := context.WithTimeout(ctx, c.cfg.RequestTimeout*4)
	defer cancel()

	var (
		resources []proxmox.Resource
		status    []proxmox.ClusterStatusEntry
		err       error
	)

	if c.cfg.CollectNodes || c.cfg.CollectGuests || c.cfg.CollectStorage {
		resources, err = c.client.ClusterResources(pollCtx)
		if err != nil {
			c.recordPollFailure(err)
			return
		}
	}
	if c.cfg.CollectCluster {
		status, err = c.client.ClusterStatus(pollCtx)
		if err != nil {
			c.recordPollFailure(err)
			return
		}
	}

	dur := time.Since(start)
	c.mu.Lock()
	c.resources = resources
	c.clusterStatus = status
	c.lastPollSuccess = time.Now()
	c.lastPollError = nil
	c.mu.Unlock()
	slog.Debug("proxsport poll ok", "duration_ms", dur.Milliseconds(), "resources", len(resources), "cluster_entries", len(status))
}

func (c *Collector) recordPollFailure(err error) {
	pollErrors++
	c.mu.Lock()
	c.lastPollError = err
	c.mu.Unlock()
	slog.Error("proxsport poll failed", "error", err, "total_failures", pollErrors)
}

// Describe implements prometheus.Collector.
func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.up
	ch <- c.scrapeDuration
	ch <- c.scrapeError
	ch <- c.clusterQuorate
	ch <- c.clusterNodes
	ch <- c.nodeUp
	ch <- c.nodeCPUUsage
	ch <- c.nodeCPUCores
	ch <- c.nodeMemoryUsed
	ch <- c.nodeMemoryTotal
	ch <- c.nodeDiskUsed
	ch <- c.nodeDiskTotal
	ch <- c.nodeUptime
	ch <- c.guestStatus
	ch <- c.guestCPUUsage
	ch <- c.guestCPUCores
	ch <- c.guestMemoryUsed
	ch <- c.guestMemoryTotal
	ch <- c.guestDiskUsed
	ch <- c.guestDiskTotal
	ch <- c.guestUptime
	ch <- c.guestNetRX
	ch <- c.guestNetTX
	ch <- c.guestDiskRead
	ch <- c.guestDiskWrite
	ch <- c.storageUsed
	ch <- c.storageTotal
	ch <- c.storageActive
}

// Collect implements prometheus.Collector. Reads the cached poll result.
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	upVal := 0.0
	if c.lastPollError == nil && !c.lastPollSuccess.IsZero() {
		upVal = 1.0
	}
	ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, upVal)
	ch <- prometheus.MustNewConstMetric(c.scrapeError, prometheus.CounterValue, float64(pollErrors))
	if !c.lastPollSuccess.IsZero() {
		ch <- prometheus.MustNewConstMetric(c.scrapeDuration, prometheus.GaugeValue, time.Since(c.lastPollSuccess).Seconds())
	}

	if c.cfg.CollectCluster {
		c.collectCluster(ch)
	}
	for _, r := range c.resources {
		switch r.Type {
		case "node":
			if c.cfg.CollectNodes {
				c.collectNode(ch, r)
			}
		case "qemu", "lxc":
			if c.cfg.CollectGuests {
				c.collectGuest(ch, r)
			}
		case "storage":
			if c.cfg.CollectStorage {
				c.collectStorage(ch, r)
			}
		}
	}
}

func (c *Collector) collectCluster(ch chan<- prometheus.Metric) {
	for _, entry := range c.clusterStatus {
		if entry.Type != "cluster" {
			continue
		}
		if entry.Quorate != nil {
			ch <- prometheus.MustNewConstMetric(c.clusterQuorate, prometheus.GaugeValue, float64(*entry.Quorate), entry.Name)
		}
		if entry.Nodes != nil {
			ch <- prometheus.MustNewConstMetric(c.clusterNodes, prometheus.GaugeValue, float64(*entry.Nodes), entry.Name)
		}
	}
}

func (c *Collector) collectNode(ch chan<- prometheus.Metric, r proxmox.Resource) {
	up := 0.0
	if r.Status == "online" {
		up = 1.0
	}
	ch <- prometheus.MustNewConstMetric(c.nodeUp, prometheus.GaugeValue, up, r.Node)
	if r.CPU != nil {
		ch <- prometheus.MustNewConstMetric(c.nodeCPUUsage, prometheus.GaugeValue, *r.CPU, r.Node)
	}
	if r.MaxCPU != nil {
		ch <- prometheus.MustNewConstMetric(c.nodeCPUCores, prometheus.GaugeValue, *r.MaxCPU, r.Node)
	}
	if r.Mem != nil {
		ch <- prometheus.MustNewConstMetric(c.nodeMemoryUsed, prometheus.GaugeValue, float64(*r.Mem), r.Node)
	}
	if r.MaxMem != nil {
		ch <- prometheus.MustNewConstMetric(c.nodeMemoryTotal, prometheus.GaugeValue, float64(*r.MaxMem), r.Node)
	}
	if r.Disk != nil {
		ch <- prometheus.MustNewConstMetric(c.nodeDiskUsed, prometheus.GaugeValue, float64(*r.Disk), r.Node)
	}
	if r.MaxDisk != nil {
		ch <- prometheus.MustNewConstMetric(c.nodeDiskTotal, prometheus.GaugeValue, float64(*r.MaxDisk), r.Node)
	}
	if r.Uptime != nil {
		ch <- prometheus.MustNewConstMetric(c.nodeUptime, prometheus.GaugeValue, float64(*r.Uptime), r.Node)
	}
}

func (c *Collector) collectGuest(ch chan<- prometheus.Metric, r proxmox.Resource) {
	// Skip templates — they have no runtime metrics and would only add noise.
	if r.Template != nil && *r.Template == 1 {
		return
	}
	labels := []string{strconv.FormatInt(r.VMID, 10), r.Name, r.Node, r.Type, r.Tags}

	running := 0.0
	if r.Status == "running" {
		running = 1.0
	}
	ch <- prometheus.MustNewConstMetric(c.guestStatus, prometheus.GaugeValue, running, labels...)

	if r.CPU != nil {
		ch <- prometheus.MustNewConstMetric(c.guestCPUUsage, prometheus.GaugeValue, *r.CPU, labels...)
	}
	if r.MaxCPU != nil {
		ch <- prometheus.MustNewConstMetric(c.guestCPUCores, prometheus.GaugeValue, *r.MaxCPU, labels...)
	}
	if r.Mem != nil {
		ch <- prometheus.MustNewConstMetric(c.guestMemoryUsed, prometheus.GaugeValue, float64(*r.Mem), labels...)
	}
	if r.MaxMem != nil {
		ch <- prometheus.MustNewConstMetric(c.guestMemoryTotal, prometheus.GaugeValue, float64(*r.MaxMem), labels...)
	}
	if r.Disk != nil {
		ch <- prometheus.MustNewConstMetric(c.guestDiskUsed, prometheus.GaugeValue, float64(*r.Disk), labels...)
	}
	if r.MaxDisk != nil {
		ch <- prometheus.MustNewConstMetric(c.guestDiskTotal, prometheus.GaugeValue, float64(*r.MaxDisk), labels...)
	}
	if r.Uptime != nil {
		ch <- prometheus.MustNewConstMetric(c.guestUptime, prometheus.GaugeValue, float64(*r.Uptime), labels...)
	}
	if r.NetIn != nil {
		ch <- prometheus.MustNewConstMetric(c.guestNetRX, prometheus.CounterValue, float64(*r.NetIn), labels...)
	}
	if r.NetOut != nil {
		ch <- prometheus.MustNewConstMetric(c.guestNetTX, prometheus.CounterValue, float64(*r.NetOut), labels...)
	}
	if r.DiskRead != nil {
		ch <- prometheus.MustNewConstMetric(c.guestDiskRead, prometheus.CounterValue, float64(*r.DiskRead), labels...)
	}
	if r.DiskWrite != nil {
		ch <- prometheus.MustNewConstMetric(c.guestDiskWrite, prometheus.CounterValue, float64(*r.DiskWrite), labels...)
	}
}

func (c *Collector) collectStorage(ch chan<- prometheus.Metric, r proxmox.Resource) {
	active := 0.0
	if r.Status == "available" {
		active = 1.0
	}
	labels := []string{r.Node, r.Storage, r.Plugin}
	ch <- prometheus.MustNewConstMetric(c.storageActive, prometheus.GaugeValue, active, labels...)
	if r.Disk != nil {
		ch <- prometheus.MustNewConstMetric(c.storageUsed, prometheus.GaugeValue, float64(*r.Disk), labels...)
	}
	if r.MaxDisk != nil {
		ch <- prometheus.MustNewConstMetric(c.storageTotal, prometheus.GaugeValue, float64(*r.MaxDisk), labels...)
	}
}

