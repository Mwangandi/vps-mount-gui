package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Server is a remote host you can pull mount targets from.
type Server struct {
	Name    string `json:"name"`
	User    string `json:"user"`
	Host    string `json:"host"`
	Port    string `json:"port"`     // SSH/SFTP port
	RDPPort string `json:"rdp_port"` // optional; defaults to 3389 if empty
}

// Target is one remote folder to mount, tied to a Server by name.
type Target struct {
	Name       string `json:"name"`
	Server     string `json:"server"` // references Server.Name
	RemotePath string `json:"remote_path"`
}

// Config is the full persisted app state.
type Config struct {
	BaseMountDir string   `json:"base_mount_dir"`
	Servers      []Server `json:"servers"`
	Targets      []Target `json:"targets"`
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
			{Name: "primary", User: "frappe", Host: "203.161.56.134", Port: "21098", RDPPort: "3389"},
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
	return cfg
}

func (c *Config) Save() error {
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

// RDPPortOrDefault returns the server's configured RDP port, or "3389" if
// it wasn't set (e.g. servers saved before this field existed).
func (s Server) RDPPortOrDefault() string {
	if s.RDPPort == "" {
		return "3389"
	}
	return s.RDPPort
}
