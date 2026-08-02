package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// targetMountPath returns the local subfolder a target mounts into,
// e.g. <BaseMountDir>/<target.Name> — so every target gets its own
// folder and multiple targets can be mounted at once.
func targetMountPath(cfg *Config, target Target) string {
	return filepath.Join(cfg.BaseMountDir, target.Name)
}

func isMounted(path string) bool {
	cmd := exec.Command("mountpoint", "-q", path)
	return cmd.Run() == nil
}

func mountTarget(cfg *Config, target Target, log func(string)) {
	server, ok := cfg.ServerByName(target.Server)
	if !ok {
		log(fmt.Sprintf("✘ Target '%s' references unknown server '%s'", target.Name, target.Server))
		return
	}
	mountPath := targetMountPath(cfg, target)

	if isMounted(mountPath) {
		log(fmt.Sprintf("⚠ '%s' already mounted at %s", target.Name, mountPath))
		return
	}

	if err := os.MkdirAll(mountPath, 0o755); err != nil {
		log(fmt.Sprintf("✘ Could not create %s: %v", mountPath, err))
		return
	}

	log(fmt.Sprintf("Mounting '%s' (%s@%s:%s) → %s ...", target.Name, server.User, server.Host, target.RemotePath, mountPath))

	remote := fmt.Sprintf("%s@%s:%s", server.User, server.Host, target.RemotePath)
	cmd := exec.Command("sshfs",
		"-p", server.Port,
		remote, mountPath,
		"-o", "reconnect",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"-o", "follow_symlinks",
	)
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) > 0 {
		log("sshfs output: " + string(out))
	}

	if isMounted(mountPath) {
		log(fmt.Sprintf("✔ Mounted '%s' at %s", target.Name, mountPath))
		openInVSCode(mountPath, log)
	} else {
		log(fmt.Sprintf("✘ Mount failed for '%s'. Check server details and SSH access.", target.Name))
	}
}

func unmountTarget(cfg *Config, target Target, log func(string)) {
	mountPath := targetMountPath(cfg, target)
	if !isMounted(mountPath) {
		log(fmt.Sprintf("⚠ '%s' is not mounted.", target.Name))
		return
	}
	log(fmt.Sprintf("Unmounting '%s'...", target.Name))
	out, err := exec.Command("fusermount", "-u", mountPath).CombinedOutput()
	if err != nil && len(out) > 0 {
		log("fusermount output: " + string(out))
	}
	if !isMounted(mountPath) {
		log(fmt.Sprintf("✔ Unmounted '%s'.", target.Name))
	} else {
		log(fmt.Sprintf("✘ Unmount failed for '%s'. Try: fusermount -uz %s", target.Name, mountPath))
	}
}
