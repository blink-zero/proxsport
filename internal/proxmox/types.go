package proxmox

// Resource is one row from /cluster/resources. The Type field disambiguates
// what the row describes: "node", "qemu", "lxc", "storage", or "sdn".
//
// Proxmox uses bytes for memory/disk, fractional 0-1 for CPU usage, and
// counters for net/disk IO. Missing fields are omitted from the response;
// pointer fields let collectors distinguish "zero" from "unset".
type Resource struct {
	Type   string `json:"type"`   // node | qemu | lxc | storage | sdn
	ID     string `json:"id"`     // e.g. "qemu/100", "node/pve1"
	Node   string `json:"node"`   // node hosting this resource (or itself)
	Status string `json:"status"` // running | stopped | online | offline | unknown

	// Identity (one of these set depending on Type)
	VMID int64  `json:"vmid,omitempty"` // qemu / lxc
	Name string `json:"name,omitempty"` // node / qemu / lxc display name

	// Usage (guests + nodes)
	CPU      *float64 `json:"cpu,omitempty"`     // ratio 0..1 of allocated CPU
	MaxCPU   *float64 `json:"maxcpu,omitempty"`  // allocated vCPUs
	Mem      *int64   `json:"mem,omitempty"`     // bytes
	MaxMem   *int64   `json:"maxmem,omitempty"`  // bytes
	Disk     *int64   `json:"disk,omitempty"`    // bytes used on root disk / storage used
	MaxDisk  *int64   `json:"maxdisk,omitempty"` // bytes allocated / storage capacity
	Uptime   *int64   `json:"uptime,omitempty"`  // seconds (guests + nodes)
	NetIn    *int64   `json:"netin,omitempty"`   // bytes counter (guests)
	NetOut   *int64   `json:"netout,omitempty"`  // bytes counter (guests)
	DiskRead *int64   `json:"diskread,omitempty"`  // bytes counter (guests)
	DiskWrite *int64  `json:"diskwrite,omitempty"` // bytes counter (guests)

	// Storage-specific
	Storage string `json:"storage,omitempty"` // storage pool name
	Plugin  string `json:"plugintype,omitempty"`

	// Misc
	Tags     string `json:"tags,omitempty"`     // semicolon-separated
	Template *int   `json:"template,omitempty"` // 1 if template, 0 otherwise
}

// ClusterStatusEntry is one row from /cluster/status. Mixed type: "cluster"
// describes the cluster itself, "node" describes each member.
type ClusterStatusEntry struct {
	Type    string `json:"type"`           // cluster | node
	ID      string `json:"id"`             // "cluster" or "node/<name>"
	Name    string `json:"name"`           // cluster name or node name
	Nodes   *int   `json:"nodes,omitempty"`   // (cluster only) total node count
	Quorate *int   `json:"quorate,omitempty"` // (cluster only) 1 if quorate
	Online  *int   `json:"online,omitempty"`  // (node only) 1 if online
	IP      string `json:"ip,omitempty"`      // (node only) management IP
	Level   string `json:"level,omitempty"`   // (cluster only) subscription level
}
