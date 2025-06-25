package main

import (
	"fmt"
	"os"
	"os/user"
	"strconv"
	"syscall"
)

func lookupUIDGID(username, groupname string) (uid, gid int, err error) {
	u, err := user.Lookup(username)
	if err != nil {
		return 0, 0, err
	}
	g, err := user.LookupGroup(groupname)
	if err != nil {
		return 0, 0, err
	}
	uid64, err := strconv.ParseInt(u.Uid, 10, 32)
	if err != nil {
		return 0, 0, err
	}
	gid64, err := strconv.ParseInt(g.Gid, 10, 32)
	if err != nil {
		return 0, 0, err
	}
	return int(uid64), int(gid64), nil
}

func dropPrivileges(appUser, appGroup string) error {
	uid, gid, err := lookupUIDGID(appUser, appGroup)
	if err != nil {
		return err
	}
	// Store current root credentials (effective UID/GID)
	rootUid := os.Geteuid()
	rootGid := os.Getegid()

	// Switch to target user, keeping root as real UID
	if err := syscall.Setreuid(rootUid, uid); err != nil {
		return err
	}
	if err := syscall.Setregid(rootGid, gid); err != nil {
		// Try to restore root privileges if setting group fails
		if restoreErr := syscall.Setreuid(uid, rootUid); restoreErr != nil {
			return fmt.Errorf("failed to set group and restore privileges: %v, %v", err, restoreErr)
		}
		return err
	}
	return nil
}

func elevatePrivileges() error {
	// Get the real UID/GID (which should be root)
	rootUid := os.Getuid()
	rootGid := os.Getgid()

	// Switch effective UID/GID back to root
	if err := syscall.Setreuid(os.Geteuid(), rootUid); err != nil {
		return err
	}
	if err := syscall.Setregid(os.Getegid(), rootGid); err != nil {
		// Try to restore previous state if setting group fails
		if restoreErr := syscall.Setreuid(rootUid, os.Geteuid()); restoreErr != nil {
			return fmt.Errorf("failed to set group and restore privileges: %v, %v", err, restoreErr)
		}
		return err
	}
	return nil
}
