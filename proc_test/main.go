package main

import (
	"log"
	"os"
	"os/exec"
	"syscall"
	"unsafe"
)

// read proc file
func readProcFile(filename string) {

	content, _ := os.ReadFile(filename)

	log.Printf("File content: %s\n", content)
}

type fdset syscall.FdSet

func (s *fdset) Sys() *syscall.FdSet {
	return (*syscall.FdSet)(s)
}

func (s *fdset) Set(fd uintptr) {
	bits := 8 * unsafe.Sizeof(s.Bits[0])
	if fd >= bits*uintptr(len(s.Bits)) {
		panic("fdset: fd out of range")
	}
	n := fd / bits
	m := fd % bits
	s.Bits[n] |= 1 << m
}

func (s *fdset) IsSet(fd uintptr) bool {
	bits := 8 * unsafe.Sizeof(s.Bits[0])
	if fd >= bits*uintptr(len(s.Bits)) {
		panic("fdset: fd out of range")
	}
	n := fd / bits
	m := fd % bits
	return s.Bits[n]&(1<<m) != 0
}

// syscall.Select file
func selectFile(filename string) {
	var fd fdset
	file, err := os.Open(filename)
	if err != nil {
		log.Fatal(err)
	}
	fd.Set(file.Fd())
	n, err := syscall.Select(int(file.Fd())+1, fd.Sys(), nil, nil, nil)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("ready %d\n", n)

	log.Printf("isset %v\n", fd.IsSet(file.Fd()))

	readProcFile(filename)

	err = exec.Command("bash", "-c", "systemctl start nginx").Run()
	if err != nil {
		panic(err)
	}
	err = exec.Command("bash", "-c", "ipvsadm -a -t 192.168.174.134:6699 -r 192.168.174.134:80 -m -w 1").Run()
	if err != nil {
		panic(err)
	}

	err = os.WriteFile(filename, []byte("hello world"), 0644)
	if err != nil {
		panic(err)
	}
}

func main() {
	selectFile(os.Args[1])
}
