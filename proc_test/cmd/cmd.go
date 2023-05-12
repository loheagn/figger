package cmd

import (
	"fmt"
	"log"
	"os/exec"
)

func Exec(command string) error {
	msg := fmt.Sprintf("exec command: %s\n", command)
	output, err := exec.Command("bash", "-c", command).CombinedOutput()
	msg += fmt.Sprintf("output: %s\n", output)
	log.Println(msg)
	return err
}
