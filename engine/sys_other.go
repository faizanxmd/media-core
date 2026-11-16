//go:build !linux

package main

func fileAlloc(dir string) string { return "none" }
