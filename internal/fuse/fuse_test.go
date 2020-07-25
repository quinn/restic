// +build darwin freebsd linux

package fuse

import (
	"bytes"
	"math/rand"
	"os"
	"testing"
	"time"

	"golang.org/x/net/context"

	"github.com/quinn/restic/internal/repository"
	"github.com/quinn/restic/internal/restic"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"

	rtest "github.com/quinn/restic/internal/test"
)

func TestCache(t *testing.T) {
	var id1, id2, id3 restic.ID
	id1[0] = 1
	id2[0] = 2
	id3[0] = 3

	const (
		kiB       = 1 << 10
		cacheSize = 64*kiB + 3*cacheOverhead
	)

	c := newBlobCache(cacheSize)

	addAndCheck := func(id restic.ID, exp []byte) {
		c.add(id, exp)
		blob, ok := c.get(id)
		rtest.Assert(t, ok, "blob %v added but not found in cache", id)
		rtest.Equals(t, &exp[0], &blob[0])
		rtest.Equals(t, exp, blob)
	}

	addAndCheck(id1, make([]byte, 32*kiB))
	addAndCheck(id2, make([]byte, 30*kiB))
	addAndCheck(id3, make([]byte, 10*kiB))

	_, ok := c.get(id2)
	rtest.Assert(t, ok, "blob %v not present", id2)
	_, ok = c.get(id1)
	rtest.Assert(t, !ok, "blob %v present, but should have been evicted", id1)

	c.add(id1, make([]byte, 1+c.size))
	_, ok = c.get(id1)
	rtest.Assert(t, !ok, "blob %v too large but still added to cache")

	c.c.Remove(id1)
	c.c.Remove(id3)
	c.c.Remove(id2)

	rtest.Equals(t, cacheSize, c.size)
	rtest.Equals(t, cacheSize, c.free)
}

func testRead(t testing.TB, f *file, offset, length int, data []byte) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := &fuse.ReadRequest{
		Offset: int64(offset),
		Size:   length,
	}
	resp := &fuse.ReadResponse{
		Data: data,
	}
	rtest.OK(t, f.Read(ctx, req, resp))
}

func firstSnapshotID(t testing.TB, repo restic.Repository) (first restic.ID) {
	err := repo.List(context.TODO(), restic.SnapshotFile, func(id restic.ID, size int64) error {
		if first.IsNull() {
			first = id
		}
		return nil
	})

	if err != nil {
		t.Fatal(err)
	}

	return first
}

func loadFirstSnapshot(t testing.TB, repo restic.Repository) *restic.Snapshot {
	id := firstSnapshotID(t, repo)
	sn, err := restic.LoadSnapshot(context.TODO(), repo, id)
	rtest.OK(t, err)
	return sn
}

func loadTree(t testing.TB, repo restic.Repository, id restic.ID) *restic.Tree {
	tree, err := repo.LoadTree(context.TODO(), id)
	rtest.OK(t, err)
	return tree
}

func TestFuseFile(t *testing.T) {
	repo, cleanup := repository.TestRepository(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	timestamp, err := time.Parse(time.RFC3339, "2017-01-24T10:42:56+01:00")
	rtest.OK(t, err)
	restic.TestCreateSnapshot(t, repo, timestamp, 2, 0.1)

	sn := loadFirstSnapshot(t, repo)
	tree := loadTree(t, repo, *sn.Tree)

	var content restic.IDs
	for _, node := range tree.Nodes {
		content = append(content, node.Content...)
	}
	t.Logf("tree loaded, content: %v", content)

	var (
		filesize uint64
		memfile  []byte
	)
	for _, id := range content {
		size, found := repo.LookupBlobSize(id, restic.DataBlob)
		rtest.Assert(t, found, "Expected to find blob id %v", id)
		filesize += uint64(size)

		buf, err := repo.LoadBlob(context.TODO(), restic.DataBlob, id, nil)
		rtest.OK(t, err)

		if len(buf) != int(size) {
			t.Fatalf("not enough bytes read for id %v: want %v, got %v", id.Str(), size, len(buf))
		}

		if uint(len(buf)) != size {
			t.Fatalf("buffer has wrong length for id %v: want %v, got %v", id.Str(), size, len(buf))
		}

		memfile = append(memfile, buf...)
	}

	t.Logf("filesize is %v, memfile has size %v", filesize, len(memfile))

	node := &restic.Node{
		Name:    "foo",
		Inode:   23,
		Mode:    0742,
		Size:    filesize,
		Content: content,
	}
	root := NewRoot(context.TODO(), repo, Config{})

	t.Logf("blob cache has %d entries", len(root.blobSizeCache.m))

	inode := fs.GenerateDynamicInode(1, "foo")
	f, err := newFile(context.TODO(), root, inode, node)
	rtest.OK(t, err)

	attr := fuse.Attr{}
	rtest.OK(t, f.Attr(ctx, &attr))

	rtest.Equals(t, inode, attr.Inode)
	rtest.Equals(t, node.Mode, attr.Mode)
	rtest.Equals(t, node.Size, attr.Size)
	rtest.Equals(t, (node.Size/uint64(attr.BlockSize))+1, attr.Blocks)

	for i := 0; i < 200; i++ {
		offset := rand.Intn(int(filesize))
		length := rand.Intn(int(filesize)-offset) + 100

		b := memfile[offset : offset+length]

		buf := make([]byte, length)

		testRead(t, f, offset, length, buf)
		if !bytes.Equal(b, buf) {
			t.Errorf("test %d failed, wrong data returned (offset %v, length %v)", i, offset, length)
		}
	}
}

// Test top-level directories for their UID and GID.
func TestTopUidGid(t *testing.T) {
	repo, cleanup := repository.TestRepository(t)
	defer cleanup()

	restic.TestCreateSnapshot(t, repo, time.Unix(1460289341, 207401672), 0, 0)

	testTopUidGid(t, Config{}, repo, uint32(os.Getuid()), uint32(os.Getgid()))
	testTopUidGid(t, Config{OwnerIsRoot: true}, repo, 0, 0)
}

func testTopUidGid(t *testing.T, cfg Config, repo restic.Repository, uid, gid uint32) {
	t.Helper()

	ctx := context.Background()
	root := NewRoot(ctx, repo, cfg)

	var attr fuse.Attr
	err := root.Attr(ctx, &attr)
	rtest.OK(t, err)
	rtest.Equals(t, uid, attr.Uid)
	rtest.Equals(t, gid, attr.Gid)

	idsdir, err := root.Lookup(ctx, "ids")
	rtest.OK(t, err)

	err = idsdir.Attr(ctx, &attr)
	rtest.OK(t, err)
	rtest.Equals(t, uid, attr.Uid)
	rtest.Equals(t, gid, attr.Gid)

	snapID := loadFirstSnapshot(t, repo).ID().Str()
	snapshotdir, err := idsdir.(fs.NodeStringLookuper).Lookup(ctx, snapID)
	rtest.OK(t, err)

	err = snapshotdir.Attr(ctx, &attr)
	rtest.OK(t, err)
	rtest.Equals(t, uid, attr.Uid)
	rtest.Equals(t, gid, attr.Gid)
}
