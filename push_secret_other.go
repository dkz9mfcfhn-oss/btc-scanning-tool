//go:build !windows

package main

func protectPush(b []byte) ([]byte, error)   { return append([]byte(nil), b...), nil }
func unprotectPush(b []byte) ([]byte, error) { return append([]byte(nil), b...), nil }
