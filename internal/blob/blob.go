// Package blob implements the `vertra blob` commands on top of the SDK's Blob
// client. Uploads show a progress bar on the terminal; downloads go through a
// temporary link and never overwrite a local file.
package blob

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"

	"github.com/vertracloud/cli/internal/ui"
	"github.com/vertracloud/sdk-api-go/vertracloud"
)

// Run dispatches the token after "blob" (ls, put, get, rm, link, stats) and
// returns a value the root command can JSON-encode. progress receives the
// upload bar; it is drawn only when it is a terminal.
func Run(ctx context.Context, client *vertracloud.Client, command string, args []string, progress io.Writer) (any, error) {
	if client == nil || client.Blob == nil {
		return nil, errors.New("blob client is nil")
	}
	b := client.Blob
	switch command {
	case "ls", "list":
		return list(ctx, b, args)
	case "put", "upload":
		return put(ctx, b, args, progress)
	case "get", "download":
		return get(ctx, b, args)
	case "rm", "remove", "delete":
		return remove(ctx, b, args)
	case "link":
		return link(ctx, b, args)
	case "stats":
		stats, err := b.Stats(ctx)
		if err != nil {
			return nil, err
		}
		return ui.Presentation{JSON: stats, Human: statsText(stats)}, nil
	default:
		return nil, fmt.Errorf("unknown blob command %q", command)
	}
}

// parse reads flags and positional arguments in any order, so that
// "put a.png --public" and "put --public a.png" both work. define registers
// the flags on fs.
func parse(name string, args []string, define func(fs *flag.FlagSet)) ([]string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	define(fs)
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

func list(ctx context.Context, b *vertracloud.BlobClient, args []string) (any, error) {
	limit := 100
	pos, err := parse("blob ls", args, func(fs *flag.FlagSet) {
		fs.IntVar(&limit, "limit", limit, "maximum number of files to show")
	})
	if err != nil {
		return nil, err
	}
	if len(pos) > 1 {
		return nil, fmt.Errorf("unexpected argument %q", pos[1])
	}
	if limit < 1 {
		return nil, errors.New("--limit must be at least 1")
	}
	prefix := ""
	if len(pos) == 1 {
		prefix = pos[0]
	}

	it := b.List(ctx, &vertracloud.BlobListOptions{Prefix: prefix, Delimiter: "/", Limit: min(limit, 1000)})
	objects := []vertracloud.BlobObject{}
	for len(objects) < limit && it.Next(ctx) {
		objects = append(objects, it.Object())
	}
	if err := it.Err(); err != nil {
		return nil, err
	}
	folders := it.Folders()
	return ui.Presentation{JSON: map[string]any{"objects": objects, "folders": folders}, Human: listText(objects, folders)}, nil
}

func put(ctx context.Context, b *vertracloud.BlobClient, args []string, progress io.Writer) (any, error) {
	public, overwrite, rename := false, false, false
	pos, err := parse("blob put", args, func(fs *flag.FlagSet) {
		fs.BoolVar(&public, "public", false, "public file")
		fs.BoolVar(&overwrite, "overwrite", false, "replace an existing file at this path")
		fs.BoolVar(&rename, "rename", false, "if the path is taken, save as name-1.ext, name-2.ext, ...")
	})
	if err != nil {
		return nil, err
	}
	if len(pos) < 1 || len(pos) > 2 {
		return nil, errors.New("use blob put <file> [path] [--public] [--overwrite | --rename]")
	}
	if overwrite && rename {
		return nil, errors.New("use either --overwrite or --rename")
	}

	file, err := os.Open(pos[0])
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", pos[0])
	}

	path := filepath.Base(pos[0])
	if len(pos) == 2 {
		path = pos[1]
	}
	visibility := vertracloud.BlobVisibilityPrivate
	if public {
		visibility = vertracloud.BlobVisibilityPublic
	}

	update, done := ui.Progress(progress, "Uploading "+path)
	obj, err := b.Put(ctx, path, file, info.Size(), &vertracloud.BlobPutOptions{
		Visibility:       visibility,
		Overwrite:        &overwrite,
		RenameOnConflict: &rename,
		OnProgress:       update,
	})
	done()
	if err != nil {
		return nil, err
	}
	return ui.Presentation{JSON: obj, Human: uploadedText(obj)}, nil
}

func get(ctx context.Context, b *vertracloud.BlobClient, args []string) (any, error) {
	pos, err := parse("blob get", args, func(*flag.FlagSet) {})
	if err != nil {
		return nil, err
	}
	if len(pos) < 1 || len(pos) > 2 {
		return nil, errors.New("use blob get <id|path> [destination]")
	}
	obj, err := resolve(ctx, b, pos[0])
	if err != nil {
		return nil, err
	}
	link, err := b.DownloadURL(ctx, obj.ID, &vertracloud.BlobDownloadOptions{ExpiresIn: 600})
	if err != nil {
		return nil, err
	}
	name := filepath.Base(obj.Path)
	if len(pos) == 2 {
		name = pos[1]
	}
	written, err := download(ctx, link.URL, name)
	if err != nil {
		return nil, err
	}
	return ui.Presentation{
		JSON:  map[string]any{"id": obj.ID, "path": name, "bytes": written},
		Human: fmt.Sprintf(t("downloaded"), ui.Plain(name), humanSize(written)),
	}, nil
}

// resolve accepts an object id (UUID) or the exact path of a file.
func resolve(ctx context.Context, b *vertracloud.BlobClient, ref string) (vertracloud.BlobObject, error) {
	if isID(ref) {
		return b.Get(ctx, ref)
	}
	it := b.List(ctx, &vertracloud.BlobListOptions{Prefix: ref})
	for it.Next(ctx) {
		if obj := it.Object(); obj.Path == ref {
			return obj, nil
		}
	}
	if err := it.Err(); err != nil {
		return vertracloud.BlobObject{}, err
	}
	return vertracloud.BlobObject{}, fmt.Errorf("no file at path %q", ref)
}

var objectIDPattern = regexp.MustCompile(`^(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func isID(ref string) bool { return objectIDPattern.MatchString(ref) }

// download writes the link's body to name. O_EXCL: an existing file is an
// error, never overwritten. A failed copy removes the partial file.
func download(ctx context.Context, link, name string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return 0, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("download failed with status %d", res.StatusCode)
	}

	out, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return 0, fmt.Errorf("%s already exists; choose another destination", name)
		}
		return 0, err
	}
	written, err := io.Copy(out, res.Body)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(name)
		return 0, err
	}
	return written, nil
}

func remove(ctx context.Context, b *vertracloud.BlobClient, args []string) (any, error) {
	// --yes is consumed by the root command's confirmation; accept it here too.
	confirmed := false
	ids, err := parse("blob rm", args, func(fs *flag.FlagSet) {
		fs.BoolVar(&confirmed, "yes", false, "skip the confirmation")
		fs.BoolVar(&confirmed, "y", false, "skip the confirmation")
	})
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, errors.New("use blob rm <id>...")
	}
	result, err := b.DeleteMany(ctx, vertracloud.BlobDeleteManyOptions{IDs: ids})
	if err != nil {
		return nil, err
	}
	return ui.Presentation{JSON: result, Human: fmt.Sprintf(t("deleted"), len(result.Deleted), len(result.NotFound))}, nil
}

func link(ctx context.Context, b *vertracloud.BlobClient, args []string) (any, error) {
	expires := 600
	pos, err := parse("blob link", args, func(fs *flag.FlagSet) {
		fs.IntVar(&expires, "expires", expires, "seconds the link stays valid (60-86400)")
	})
	if err != nil {
		return nil, err
	}
	if len(pos) != 1 {
		return nil, errors.New("use blob link <id> [--expires <seconds>]")
	}
	if expires < 60 || expires > 86400 {
		return nil, errors.New("--expires must be between 60 and 86400 seconds")
	}
	link, err := b.DownloadURL(ctx, pos[0], &vertracloud.BlobDownloadOptions{ExpiresIn: expires})
	if err != nil {
		return nil, err
	}
	return ui.Presentation{JSON: link, Human: linkText(link)}, nil
}
