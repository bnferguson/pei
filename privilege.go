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
		// Restore both UID and GID to prevent partial privilege state
		// Attempting to restore UID to rootUid after Setregid failure.
		fmt.Printf("Setregid failed: %v. Attempting to restore UID to %d.\n", err, rootUid)
		if restoreUidErr := syscall.Setreuid(uid, rootUid); restoreUidErr != nil {
			return fmt.Errorf("failed to set group and restore UID: %v, %v", err, restoreUidErr)
		}
		// Note: GID should already be at rootGid since Setregid failed
		return err
	}
	return nil
}

func elevatePrivileges() error {
	// Get the real UID/GID (which should be root)
	rootUid := os.Getuid()
	rootGid := os.Getgid()

	// Store current effective UID/GID for potential restoration
	prevEffectiveUid := os.Geteuid()
	prevEffectiveGid := os.Getegid()

	// Switch effective UID/GID back to root
	if err := syscall.Setreuid(prevEffectiveUid, rootUid); err != nil {
		return err
	}
	if err := syscall.Setregid(prevEffectiveGid, rootGid); err != nil {
		// Try to restore previous state if setting group fails
		// Restore both UID and GID to prevent partial privilege state
		if restoreUidErr := syscall.Setreuid(rootUid, prevEffectiveUid); restoreUidErr != nil {
			return fmt.Errorf("failed to set group and restore UID: %v, %v", err, restoreUidErr)
		}
		// Note: GID should already be at prevEffectiveGid since Setregid failed
		return err
	}
	return nil
}
