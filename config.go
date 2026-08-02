package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Server is a remote host you can pull mount targets from.
type Server struct {
	Name          string   `json:"name"`
	User          string   `json:"user"`
	Host          string   `json:"host"`
	Port          string   `json:"port"`     // SSH/SFTP port
	RDPPort       string   `json:"rdp_port"` // optional; defaults to 3389 if empty
	Favorite      bool     `json:"favorite"`
	LastConnected string   `json:"last_connected"` // RFC3339, empty = never
	Bookmarks     []string `json:"bookmarks"`
}

// Target is one remote folder to mount, tied to a Server by name.
type Target struct {
	Name       string `json:"name"`
	Server     string `json:"server"` // references Server.Name
	RemotePath string `json:"remote_path"`
}

// Config is the full persisted app state.
type Config struct {
	BaseMountDir   string   `json:"base_mount_dir"`
	Servers        []Server `json:"servers"`
	Targets        []Target `json:"targets"`
	LocalBookmarks []string `json:"local_bookmarks"`
}

func configPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "vps-mount-gui", "config.json")
}

func defaultConfig() Config {
	home, _ := os.UserHomeDir()
	return Config{
		BaseMountDir: filepath.Join(home, "Desktop", "VPS"),
		Servers: []Server{
			{Name: "primary", User: "frappe", Host: "203.161.56.134", Port: "21098", RDPPort: "3389", Favorite: true},
		},
		Targets: []Target{
			{Name: "helpdesk", Server: "primary", RemotePath: "/home/frappe/helpdesk"},
			{Name: "website", Server: "primary", RemotePath: "/home/frappe/County-Website-main"},
			{Name: "grm", Server: "primary", RemotePath: "/home/frappe/GRM/GRM-TTCG"},
			{Name: "vtc", Server: "primary", RemotePath: "/home/frappe/VTC-Web-Portal/"},
			{Name: "cpits", Server: "primary", RemotePath: "/home/frappe/project_management"},
			{Name: "resolution", Server: "primary", RemotePath: "/home/frappe/CA-Resolution-Tracker"},
			{Name: "edms", Server: "primary", RemotePath: "/home/frappe/TTEDMS"},
		},
		LocalBookmarks: []string{},
	}
}

func (c *Config) normalize() {
	if c.LocalBookmarks == nil {
		c.LocalBookmarks = []string{}
	}
	for i := range c.Servers {
		if c.Servers[i].Bookmarks == nil {
			c.Servers[i].Bookmarks = []string{}
		}
	}
}

// LoadConfig reads the config file, seeding it with defaults on first run.
func LoadConfig() Config {
	path := configPath()
	data, err := os.ReadFile(path)
	if err != nil {
		cfg := defaultConfig()
		_ = cfg.Save()
		return cfg
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		cfg := defaultConfig()
		_ = cfg.Save()
		return cfg
	}
	cfg.normalize()
	return cfg
}

func (c *Config) Save() error {
	c.normalize()
	path := configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (c *Config) ServerByName(name string) (Server, bool) {
	for _, s := range c.Servers {
		if s.Name == name {
			return s, true
		}
	}
	return Server{}, false
}

func (c *Config) ServerNames() []string {
	names := make([]string, 0, len(c.Servers))
	for _, s := range c.Servers {
		names = append(names, s.Name)
	}
	return names
}

func (c *Config) MarkServerUsed(name string) {
	for i := range c.Servers {
		if c.Servers[i].Name == name {
			c.Servers[i].LastConnected = time.Now().UTC().Format(time.RFC3339)
			_ = c.Save()
			return
		}
	}
}

func (c *Config) ToggleFavorite(name string) {
	for i := range c.Servers {
		if c.Servers[i].Name == name {
			c.Servers[i].Favorite = !c.Servers[i].Favorite
			_ = c.Save()
			return
		}
	}
}

func (c *Config) Favorites() []Server {
	out := make([]Server, 0)
	for _, s := range c.Servers {
		if s.Favorite {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].LastConnected == out[j].LastConnected {
			return out[i].Name < out[j].Name
		}
		return out[i].LastConnected > out[j].LastConnected
	})
	return out
}

func (c *Config) RecentServers(max int) []Server {
	out := make([]Server, 0)
	for _, s := range c.Servers {
		if s.Favorite || s.LastConnected == "" {
			continue
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].LastConnected == out[j].LastConnected {
			return out[i].Name < out[j].Name
		}
		return out[i].LastConnected > out[j].LastConnected
	})
	if max > 0 && len(out) > max {
		out = out[:max]
	}
	return out
}

func (c *Config) AddLocalBookmark(path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	for _, existing := range c.LocalBookmarks {
		if existing == path {
			return
		}
	}
	c.LocalBookmarks = append(c.LocalBookmarks, path)
	_ = c.Save()
}

func (c *Config) RemoveLocalBookmark(path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	filtered := make([]string, 0, len(c.LocalBookmarks))
	for _, existing := range c.LocalBookmarks {
		if existing != path {
			filtered = append(filtered, existing)
		}
	}
	c.LocalBookmarks = filtered
	_ = c.Save()
}

func (c *Config) AddRemoteBookmark(serverName, path string) {
	serverName = strings.TrimSpace(serverName)
	path = strings.TrimSpace(path)
	if serverName == "" || path == "" {
		return
	}
	for i := range c.Servers {
		if c.Servers[i].Name != serverName {
			continue
		}
		for _, existing := range c.Servers[i].Bookmarks {
			if existing == path {
				return
			}
		}
		c.Servers[i].Bookmarks = append(c.Servers[i].Bookmarks, path)
		_ = c.Save()
		return
	}
}

func (c *Config) RemoveRemoteBookmark(serverName, path string) {
	serverName = strings.TrimSpace(serverName)
	path = strings.TrimSpace(path)
	if serverName == "" || path == "" {
		return
	}
	for i := range c.Servers {
		if c.Servers[i].Name != serverName {
			continue
		}
		filtered := make([]string, 0, len(c.Servers[i].Bookmarks))
		for _, existing := range c.Servers[i].Bookmarks {
			if existing != path {
				filtered = append(filtered, existing)
			}
		}
		c.Servers[i].Bookmarks = filtered
		_ = c.Save()
		return
	}
}

// RDPPortOrDefault returns the server's configured RDP port, or "3389" if
// it wasn't set (e.g. servers saved before this field existed).
func (s Server) RDPPortOrDefault() string {
	if s.RDPPort == "" {
		return "3389"
	}
	return s.RDPPort
}
