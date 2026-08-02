package main

import (
	"fmt"
	"os/exec"
)

// launchRDPWindow opens an RDP session to the given server in a separate,
// detached client window (its own OS window/process), using whatever RDP
// client is available on the system.
func launchRDPWindow(s Server, log func(string)) {
	port := s.RDPPortOrDefault()
	address := fmt.Sprintf("%s:%s", s.Host, port)

	type launcher struct {
		bin  string
		args []string
	}
	candidates := []launcher{
		// FreeRDP: modern builds show their own graphical login prompt if no
		// /p: password is given, and /cert:ignore skips the cert-trust dialog
		// for self-signed certs (common on internal boxes).
		{"xfreerdp", []string{"/v:" + address, "/u:" + s.User, "/cert:ignore", "/dynamic-resolution"}},
		{"xfreerdp3", []string{"/v:" + address, "/u:" + s.User, "/cert:ignore", "/dynamic-resolution"}},
		{"rdesktop", []string{"-u", s.User, address}},
		{"remmina", []string{"-c", fmt.Sprintf("rdp://%s@%s", s.User, address)}},
		{"vinagre", []string{fmt.Sprintf("rdp://%s@%s", s.User, address)}},
	}

	for _, c := range candidates {
		path, err := exec.LookPath(c.bin)
		if err != nil {
			continue
		}
		cmd := exec.Command(path, c.args...)
		if err := cmd.Start(); err != nil {
			log(fmt.Sprintf("✘ Found %s but couldn't launch it: %v", c.bin, err))
			continue
		}
		log(fmt.Sprintf("Opened RDP session to %s in %s.", address, c.bin))
		return
	}
	log("✘ No RDP client found on this system (tried xfreerdp, rdesktop, remmina, " +
		"vinagre). Install one, e.g.: sudo apt install freerdp2-x11")
}
