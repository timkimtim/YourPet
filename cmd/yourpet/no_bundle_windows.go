//go:build !bundled

package main

import _ "embed"

//go:embed sample.pet
var bundledPet []byte
