package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/model"
)

func EntityList(ctx *Context, opts struct {
	FormatOptions
	ConfigCentric
	Attribute string `short:"a" long:"attribute" description:"Attribute to filter by"`
	Value     string `short:"V" long:"value" description:"Value to filter by"`
	Kind      string `short:"k" long:"kind" description:"Kind of entity to filter by"`
	Limit     int64  `short:"n" long:"limit" description:"Max entities to show, 0 for all" default:"50"`
	After     string `long:"after" description:"Resume from the cursor printed by a previous page"`
	Detail    bool   `short:"l" long:"detail" description:"Show each entity in full instead of one line each"`
	Expand    bool   `long:"expand" description:"Show nested components in full"`
	Columns   string `long:"columns" description:"Comma-separated fields to use as table columns"`
	// A saga's JSON blobs are unbounded in principle and one is enough to bury
	// a listing, so the text view caps them. --json never applies the cap.
	MaxValue int    `long:"max-value-len" description:"Elide values longer than this, 0 for no limit" default:"120"`
	Address  string `long:"address" description:"Address to listen on" default:"localhost:8443"`
}) error {
	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	eac := entityserver_v1alpha.NewEntityAccessClient(client)

	var (
		index entity.Attr
		// Only a --kind filter names a facet. MakeAttr can hand back a value of
		// any kind, and Value.Id panics on anything that is not a reference.
		filterKind string
	)

	if opts.Kind != "" {
		res, err := eac.LookupKind(ctx, opts.Kind)
		if err != nil {
			return err
		}

		index = res.Attr()
		if index.Value.Kind() == entity.KindId {
			filterKind = string(index.Value.Id())
		}
	} else {
		indexres, err := eac.MakeAttr(ctx, opts.Attribute, opts.Value)
		if err != nil {
			return err
		}

		index = indexres.Attr()
	}

	// JSON is the contract scripts parse, so it is never elided. The text view
	// is free to be opinionated precisely because this path exists.
	maxValue := int64(opts.MaxValue)
	if opts.IsJSON() {
		maxValue = 0
	}

	docs, cursor, total, err := fetchDocuments(ctx, eac, index, opts.After, opts.Limit, maxValue)
	if err != nil {
		return err
	}

	if opts.IsJSON() {
		out, err := json.MarshalIndent(docs, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to encode documents: %w", err)
		}

		_, err = ctx.Stdout.Write(append(out, '\n'))
		return err
	}

	if len(docs) == 0 {
		fmt.Fprintln(ctx.Stderr, "No entities matched.")
		return nil
	}

	renderOpts := model.RenderOptions{Expand: opts.Expand}

	if opts.Detail {
		for i, doc := range docs {
			if i > 0 {
				fmt.Fprintln(ctx.Stdout)
			}

			model.RenderCard(ctx.Stdout, doc, renderOpts)
		}
	} else {
		var explicit []string
		for _, c := range strings.Split(opts.Columns, ",") {
			if c = strings.TrimSpace(c); c != "" {
				explicit = append(explicit, c)
			}
		}

		columns := model.TableColumns(docs, filterKind, explicit)
		model.RenderTable(ctx.Stdout, docs, columns, renderOpts)
	}

	elided := false
	for _, doc := range docs {
		if doc.Elided() {
			elided = true
			break
		}
	}

	printListFooter(ctx.Stderr, len(docs), total, cursor, opts.Detail)
	printElisionHint(ctx.Stderr, elided)

	return nil
}

// printElisionHint names the way out of a shortened value. The defaults exist
// so nobody gets blasted by an entity they had no way to know was huge, which
// only works if the way to see the whole thing is right there when it happens.
func printElisionHint(w io.Writer, elided bool) {
	if !elided {
		return
	}

	fmt.Fprintln(w, "Some values were shortened; --max-value-len 0 shows them whole.")
}

// fetchDocuments walks pages until it has what the caller asked for.
//
// The server caps how much it will render into one response, so "give me
// everything" is a walk rather than a single enormous request. Doing that here
// keeps --limit 0 meaning every entity, which the security audit depends on,
// without the server ever holding the whole index in memory at once.
func fetchDocuments(
	ctx context.Context,
	eac *entityserver_v1alpha.EntityAccessClient,
	index entity.Attr,
	after string,
	limit int64,
	maxValue int64,
) ([]*model.Document, string, int64, error) {
	var (
		docs  []*model.Document
		total int64
		first = true
	)

	for {
		// Zero asks the server for as much as it is willing to render.
		var want int64
		if limit > 0 {
			want = limit - int64(len(docs))
		}

		res, err := eac.ListDocuments(ctx, index, after, want, maxValue)
		if err != nil {
			return nil, "", 0, err
		}

		page, err := decodeDocuments(res.Documents())
		if err != nil {
			return nil, "", 0, err
		}

		docs = append(docs, page...)

		if first {
			total = res.Total()
			first = false
		}

		after = res.Cursor()

		switch {
		case after == "":
			// Index exhausted.
			return docs, "", total, nil
		case limit > 0 && int64(len(docs)) >= limit:
			// Got what was asked for; hand back where to resume.
			return docs, after, total, nil
		case len(page) == 0:
			// A cursor that yields nothing would spin forever.
			return docs, after, total, nil
		}
	}
}

// decodeDocuments preserves numbers exactly. Field values are untyped, so a
// plain decode would round an int64 through float64 and quietly corrupt a large
// one on the way back out to --json.
func decodeDocuments(raw []byte) ([]*model.Document, error) {
	var docs []*model.Document
	if err := decodeExact(raw, &docs); err != nil {
		return nil, err
	}

	return docs, nil
}

// decodeExact is the only way documents should be decoded. Every read path has
// to use it, or the same entity renders differently depending on which command
// you asked.
func decodeExact(raw []byte, into any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()

	if err := dec.Decode(into); err != nil {
		return fmt.Errorf("failed to decode documents: %w", err)
	}

	return nil
}

// printListFooter tells you what you are not looking at. It goes to stderr so a
// pipe only ever receives the listing itself.
func printListFooter(w io.Writer, shown int, total int64, cursor string, detail bool) {
	var parts []string

	if total > int64(shown) {
		parts = append(parts, fmt.Sprintf("%d of %d entities", shown, total))
	} else if shown == 1 {
		parts = append(parts, "1 entity")
	} else {
		parts = append(parts, fmt.Sprintf("%d entities", shown))
	}

	if cursor != "" {
		parts = append(parts, fmt.Sprintf("more: --after %s", strings.TrimSpace(cursor)))
	}

	if !detail {
		parts = append(parts, "-l for detail")
	}

	parts = append(parts, "--json to parse")

	fmt.Fprintf(w, "\n%s\n", strings.Join(parts, " · "))
}
