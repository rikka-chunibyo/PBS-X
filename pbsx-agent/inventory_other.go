//go:build !linux && !windows

package main

func collectInventory() inventory {
	return inventory{}
}
