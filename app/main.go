package main

import (
	"fmt"
	"path/filepath"
	"syscall"

	// Uncomment this block to pass the first stage!
	"os"
	"os/exec"
)

// Usage: your_docker.sh run <image> <command> <arg1> <arg2> ...
func main() {
	command := os.Args[3]
	args := os.Args[4:len(os.Args)]

	// create tmp directory
	tmp_dir, err := os.MkdirTemp("", "sandbox_*")
	if err != nil {
		fmt.Printf("Error creating tmp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.Remove(tmp_dir)

	// chmod 0755 temp directory
	err = os.Chmod(tmp_dir, 0755)
	if err != nil {
		fmt.Printf("Error chmod: %v\n", err)
		os.Exit(1)
	}

	// mkDirAll filepath.join(tmp_dir, /usr/local/bin) 0755
	err = os.MkdirAll(filepath.Join(tmp_dir, "/usr/local/bin"), 0755)
	if err != nil {
		fmt.Printf("Error mkdirall: %v\n", err)
		os.Exit(1)
	}

	// os.Link(docker-explorer full path, filepathjoin(tempDir, "/usr/local/bin", "docker-explorer"))
	err = os.Link("/usr/local/bin/docker-explorer", filepath.Join(tmp_dir, "/usr/local/bin", "docker-explorer"))
	if err != nil {
		fmt.Printf("error: %v\n", err)
		os.Exit(1)
	}

	//chroot temp_dir
	err = syscall.Chroot(tmp_dir)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		os.Exit(1)
	}

	//chdir into /
	err = os.Chdir("/")
	if err != nil {
		fmt.Printf("error: %v\n", err)
		os.Exit(1)
	}

	cmd := exec.Command(command, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID,
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		if exiterr, ok := err.(*exec.ExitError); ok {
			os.Exit(exiterr.ExitCode())
		}
	}
}
