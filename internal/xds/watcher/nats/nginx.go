package nats

import (
	"bytes"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strings"
)

func checkNginxIsRunning() bool {
	cmd := exec.Command("systemctl", "is-active", "nginx")
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return false
	}

	output := strings.TrimSpace(out.String())
	if output == "active" {
		return true
	} else {
		return false
	}
}

func writeNginxConfig(filename string, configContent string) error {
	// Write the Nginx configuration file to disk
	configFile := "/etc/nginx/sites-available/" + filename
	err := ioutil.WriteFile(configFile, []byte(configContent), 0644)
	if err != nil {
		return err
	}

	// Create a symbolic link from the configuration file to the sites-enabled folder
	symlinkPath := "/etc/nginx/sites-enabled/" + filename
	err = os.Symlink(configFile, symlinkPath)
	if err != nil {
		log.Printf("Error creating symlink: %s. Continue", err)
	}

	// Define the command to restart Nginx
	cmd := exec.Command("systemctl", "restart", "nginx")
	// Execute the command and wait for it to finish
	err = cmd.Run()
	if err != nil {
		return err
	}

	return nil
}
