// +build darwin freebsd linux

package fuse

import (
	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	"github.com/quinn/restic/internal/restic"
	"golang.org/x/net/context"
)

// Statically ensure that *link implements the given interface
var _ = fs.NodeReadlinker(&link{})

type link struct {
	root  *Root
	node  *restic.Node
	inode uint64
}

func newLink(ctx context.Context, root *Root, inode uint64, node *restic.Node) (*link, error) {
	return &link{root: root, inode: inode, node: node}, nil
}

func (l *link) Readlink(ctx context.Context, req *fuse.ReadlinkRequest) (string, error) {
	return l.node.LinkTarget, nil
}

func (l *link) Attr(ctx context.Context, a *fuse.Attr) error {
	a.Inode = l.inode
	a.Mode = l.node.Mode

	if !l.root.cfg.OwnerIsRoot {
		a.Uid = l.node.UID
		a.Gid = l.node.GID
	}
	a.Atime = l.node.AccessTime
	a.Ctime = l.node.ChangeTime
	a.Mtime = l.node.ModTime

	a.Nlink = uint32(l.node.Links)

	return nil
}
