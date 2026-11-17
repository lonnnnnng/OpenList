//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package product

import "syscall"

// RestrictNewFiles 让WAL、日志和轮转凭据从创建时就私有，不能只在启动后补chmod。作者：long
func RestrictNewFiles() { syscall.Umask(0o077) }
