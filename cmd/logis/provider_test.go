package main

import (
	"os"
	"reflect"
	"slices"
	"testing"
)

// The launch carries the provider's argv, the client's working directory
// and its environment, all as bytes.
func TestTheLaunchCarriesArgvCwdAndEnvironmentAsBytes(t *testing.T) {
	l := providerLaunch([]string{"rite", "\xff"})
	if !reflect.DeepEqual(l.Argv, [][]byte{[]byte("rite"), []byte("\xff")}) {
		t.Fatalf("%q", l.Argv)
	}
	cwd, err := os.Getwd()
	if err != nil || string(l.Cwd) != cwd {
		t.Fatalf("%q, %v", l.Cwd, err)
	}
	want := []byte("PATH=" + os.Getenv("PATH"))
	if !slices.ContainsFunc(l.Env, func(kv []byte) bool { return string(kv) == string(want) }) {
		t.Fatalf("no %q in the environment", want)
	}
}
