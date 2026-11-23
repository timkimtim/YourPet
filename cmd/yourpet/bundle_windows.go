//go:build bundled

package main

import _ "embed"

//go:embed default.pet
var bundledPet []byte
