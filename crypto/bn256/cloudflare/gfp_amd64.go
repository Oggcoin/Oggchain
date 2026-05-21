// +build amd64,!appengine,!gccgo

package bn256

// hasBMI2 is read by the assembly in gfp_amd64.s.
// runtime.support_bmi2 was removed in Go 1.17; hardcoded true since BMI2 has
// been standard on all x86-64 CPUs since Intel Haswell (2013) / AMD Excavator (2015).
// Change to false only if targeting very old hardware.
var hasBMI2 = true

// go:noescape
func gfpNeg(c, a *gfP)

//go:noescape
func gfpAdd(c, a, b *gfP)

//go:noescape
func gfpSub(c, a, b *gfP)

//go:noescape
func gfpMul(c, a, b *gfP)
