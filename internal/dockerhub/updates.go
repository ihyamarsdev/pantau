package dockerhub

import (
	"context"
	"errors"
	"strings"
	"sync"
)

const (
	checkWorkers = 4
	// Docker Hub allows about 180 API requests per minute per IP; one check stays well under it.
	maxLookups = 100
)

// Local is one image on a host: its ID, its repo:tag names and the registry digests the engine recorded
// (repo@sha256:…, from `docker image inspect`).
type Local struct {
	ID      string
	Tags    []string
	Digests []string
}

// Update is the Docker Hub state of one local repo:tag.
type Update struct {
	Ref          string `json:"ref"`
	ImageID      string `json:"image_id"`
	Repo         string `json:"repo"`   // Docker Hub namespace/name
	Status       string `json:"status"` // up_to_date | update_available | not_found | unknown | error
	Detail       string `json:"detail,omitempty"`
	RemoteDigest string `json:"remote_digest,omitempty"`
	RemotePushed string `json:"remote_pushed,omitempty"`
}

type tagKey struct{ ns, name, tag string }

type lookup struct {
	tag *Tag
	err error
}

// CheckUpdates compares every Docker Hub repo:tag of images with the digest Docker Hub currently serves for that
// tag. Images of other registries are left out. Lookups are deduplicated, run a few at a time, and stop at the
// first failure other than "tag not found" (unreachable, rate limited), which is then reported for the rest.
func (c *Client) CheckUpdates(ctx context.Context, images []Local) []Update {
	type item struct {
		u     Update
		key   tagKey
		local map[string]bool
	}
	var items []item
	lookups := map[tagKey]*lookup{}
	var order []tagKey
	for _, img := range images {
		for _, ref := range img.Tags {
			ns, name, tag, ok := ParseRef(ref)
			if !ok {
				continue
			}
			it := item{u: Update{Ref: ref, ImageID: img.ID, Repo: ns + "/" + name}, key: tagKey{ns, name, tag}, local: map[string]bool{}}
			for _, d := range img.Digests {
				repo, digest, found := strings.Cut(d, "@")
				if dns, dname, ok := hubRepo(repo); found && ok && dns == ns && dname == name {
					it.local[digest] = true
				}
			}
			if len(it.local) > 0 {
				if _, seen := lookups[it.key]; !seen {
					lookups[it.key] = &lookup{}
					order = append(order, it.key)
				}
			}
			items = append(items, it)
		}
	}

	c.runLookups(ctx, order, lookups)

	out := make([]Update, 0, len(items))
	for _, it := range items {
		u := it.u
		l := lookups[it.key]
		switch {
		case len(it.local) == 0:
			u.Status, u.Detail = "unknown", "no registry digest recorded (image built or loaded locally)"
		case IsNotFound(l.err):
			u.Status, u.Detail = "not_found", "this tag does not exist on Docker Hub"
		case errors.Is(l.err, context.DeadlineExceeded):
			u.Status, u.Detail = "error", "update check timed out"
		case l.err != nil:
			u.Status, u.Detail = "error", l.err.Error()
		default:
			u.RemoteDigest, u.RemotePushed = l.tag.Digest, l.tag.LastPushed
			u.Status = "update_available"
			if l.tag.servesAny(it.local) {
				u.Status = "up_to_date"
			}
		}
		out = append(out, u)
	}
	return out
}

// servesAny reports whether the tag currently resolves to one of the local digests: the index digest a pull by
// tag records, or the digest of one of its platform manifests.
func (t *Tag) servesAny(local map[string]bool) bool {
	if local[t.Digest] {
		return true
	}
	for _, d := range t.platformDigests {
		if local[d] {
			return true
		}
	}
	return false
}

func (c *Client) runLookups(ctx context.Context, order []tagKey, lookups map[tagKey]*lookup) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		mu      sync.Mutex
		stopErr error
		wg      sync.WaitGroup
		sem     = make(chan struct{}, checkWorkers)
	)
	for i, k := range order {
		l := lookups[k]
		if i >= maxLookups {
			l.err = errors.New("not checked: more than 100 Docker Hub tags in one check")
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			t, err := c.Tag(ctx, k.ns, k.name, k.tag)
			mu.Lock()
			defer mu.Unlock()
			l.tag, l.err = t, err
			if err != nil && !IsNotFound(err) && stopErr == nil && !errors.Is(err, context.Canceled) {
				stopErr = err
				cancel()
			}
		}()
	}
	wg.Wait()
	for _, l := range lookups {
		if stopErr != nil && errors.Is(l.err, context.Canceled) {
			l.err = stopErr
		}
	}
}
