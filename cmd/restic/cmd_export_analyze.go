package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"

	"github.com/davecgh/go-spew/spew"
	"github.com/quinn/restic/internal/restic"
	"github.com/quinn/restic/walker"

	"github.com/gabriel-vasile/mimetype"
	"github.com/spf13/cobra"

	_ "github.com/go-kivik/couchdb/v4" // The CouchDB driver
	kivik "github.com/go-kivik/kivik/v4"
)

var cmd = &cobra.Command{
	Use:               "export-analyze",
	Short:             "-",
	Long:              `MISSING`,
	DisableAutoGenTag: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runExportAnalyze(cmdOptions, globalOptions, args)
	},
}

// CmdOptions collects all options for the dump command.
type CmdOptions struct {
	Hosts []string
	Paths []string
	Tags  restic.TagLists
}

var cmdOptions CmdOptions
var db *kivik.DB

func init() {
	cmdRoot.AddCommand(cmd)

	client, err := kivik.New("couch", "http://admin:admin@localhost:5984/")

	if err != nil {
		panic(err)
	}

	if err != nil {
		panic(err)
	}

	db = client.DB(context.TODO(), "archive")
}

// ArchiveDoc is the struct for unmarshaling from couchdb
type ArchiveDoc struct {
	ID   string   `json:"_id"`
	Rev  string   `json:"_rev,omitempty"`
	Path string   `json:"path"`
	Mime []string `json:"mime"`
}

func runExportAnalyze(opts CmdOptions, gopts GlobalOptions, args []string) error {
	ctx := gopts.ctx

	repo, err := OpenRepository(gopts)
	if err != nil {
		return err
	}

	err = repo.LoadIndex(ctx)
	if err != nil {
		return err
	}

	var id restic.ID

	id, err = restic.FindLatestSnapshot(ctx, repo, opts.Paths, opts.Tags, opts.Hosts)
	if err != nil {
		Exitf(1, "latest snapshot for criteria not found: %v Paths:%v Hosts:%v", err, opts.Paths, opts.Hosts)
	}

	sn, err := restic.LoadSnapshot(gopts.ctx, repo, id)
	if err != nil {
		Exitf(2, "loading snapshot %q failed: %v", snapshotIDString, err)
	}

	fmt.Printf("using snapshot %v", sn.ID())

	err = walker.Walk(ctx, repo, *sn.Tree, nil, func(_ restic.ID, nodepath string, node *restic.Node, err error) (bool, error) {
		if node == nil || node.Type != "file" {
			return false, nil
		}

		var mimes []string
		mime, err := mimetype.DetectFile(nodepath)

		if err != nil {
			fmt.Println("Missing:", nodepath)
		} else {
			if mime.String() == "application/octet-stream" {
				fmt.Println(nodepath)
				spew.Dump(mime)
			}

			for mime != nil {
				mimes = append(mimes, mime.String())
				mime = mime.Parent()
			}
		}

		for _, id := range node.Content {
			encodedID := fmt.Sprintf("%v", id)
			var doc ArchiveDoc

			docRef := db.Get(ctx, encodedID)

			if docRef.Err == nil {
				currentDocData, err := ioutil.ReadAll(docRef.Body)

				if err != nil {
					return false, err
				}

				json.Unmarshal(currentDocData, &doc)
			}

			doc.ID = encodedID
			doc.Path = nodepath
			doc.Mime = mimes

			rev, err := db.Put(ctx, encodedID, doc)

			if err != nil {
				return false, err
			}

			fmt.Printf("processed %s %s\n", nodepath, rev)
		}

		return false, nil
	})

	if err != nil {
		return err
	}

	return nil
}
