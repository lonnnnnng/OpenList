//go:build !(aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris)

package product

// RestrictNewFiles 非首发平台保留可编译入口，文件权限仍由显式操作校验。作者：long
func RestrictNewFiles() {}
