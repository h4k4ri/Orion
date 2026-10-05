//go:build !libvirt

package main

import "log"

func main() {
	log.Fatal("kvm plugin requires the libvirt build tag and libvirt/pkg-config dependencies; build with -tags libvirt")
}
