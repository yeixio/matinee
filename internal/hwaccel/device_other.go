//go:build !unix

package hwaccel

// deviceProblem only applies to render nodes, which are a Linux thing.
func deviceProblem(string) string { return "" }
