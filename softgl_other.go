//go:build !linux

package main

func useSoftwareGLIfNeeded()    {}
func retryWithSoftwareGL(error) {}
