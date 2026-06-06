package main

import "testing"

func TestNewLoggerNotNil(t *testing.T) {
	if newLogger() == nil {
		t.Fatal("newLogger returned nil")
	}
}
