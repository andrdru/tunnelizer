package tunnel

import (
	"strconv"

	"github.com/andrdru/tunnelizer/internal/config"
)

const (
	SSHBinary = "ssh"

	serverAliveInterval = "15"
	serverAliveCountMax = "3"
	localBind           = "127.0.0.1"
	exitOnForwardFail   = "ExitOnForwardFailure=yes"
	batchMode           = "BatchMode=yes"
	controlMasterNo     = "ControlMaster=no"
	controlPersistYes   = "ControlPersist=yes"

	slaveArgsCap  = 14
	masterArgsCap = 12
	targetArgsCap = 5
)

func SlaveArgs(t config.ResolvedTunnel, sockPath string) []string {
	args := make([]string, 0, slaveArgsCap)
	args = append(args,
		"-N",
		"-L", forwardSpec(t),
		"-o", exitOnForwardFail,
		"-o", "ServerAliveInterval="+serverAliveInterval,
		"-o", "ServerAliveCountMax="+serverAliveCountMax,
		"-o", batchMode,
	)

	if t.Interactive {
		args = append(args, "-S", sockPath, "-o", controlMasterNo)
	}

	return append(args, targetArgs(t)...)
}

func MasterArgs(t config.ResolvedTunnel, sockPath string) []string {
	args := make([]string, 0, masterArgsCap)
	args = append(args,
		"-f", "-N", "-M",
		"-S", sockPath,
		"-o", controlPersistYes,
		"-o", exitOnForwardFail,
		"-o", "ServerAliveInterval="+serverAliveInterval,
		"-o", "ServerAliveCountMax="+serverAliveCountMax,
	)

	return append(args, targetArgs(t)...)
}

func CheckArgs(t config.ResolvedTunnel, sockPath string) []string {
	return append([]string{"-S", sockPath, "-O", "check"}, targetArgs(t)...)
}

func ExitArgs(t config.ResolvedTunnel, sockPath string) []string {
	return append([]string{"-S", sockPath, "-O", "exit"}, targetArgs(t)...)
}

func forwardSpec(t config.ResolvedTunnel) string {
	return localBind + ":" + strconv.Itoa(t.LocalPort) + ":" + t.RemoteHost + ":" + strconv.Itoa(t.RemotePort)
}

func targetArgs(t config.ResolvedTunnel) []string {
	args := make([]string, 0, targetArgsCap)

	if t.IdentityFile != "" {
		args = append(args, "-i", t.IdentityFile)
	}

	if t.Port != 0 {
		args = append(args, "-p", strconv.Itoa(t.Port))
	}

	target := t.Host
	if t.User != "" {
		target = t.User + "@" + t.Host
	}

	return append(args, target)
}
