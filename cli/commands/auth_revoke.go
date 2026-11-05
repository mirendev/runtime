package commands

import (
	"fmt"
	"math/big"
	"strings"

	"miren.dev/runtime/api/certrevocation/certrevocation_v1alpha"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/ui"
)

type revocationJSON struct {
	Serial    string `json:"serial"`
	Decimal   string `json:"serial_decimal"`
	RevokedAt string `json:"revoked_at"`
	RevokedBy string `json:"revoked_by"`
	Reason    string `json:"reason,omitempty"`
}

func newRevocationJSON(r *certrevocation_v1alpha.Revocation) revocationJSON {
	return revocationJSON{
		Serial:    r.Serial(),
		Decimal:   serialDecimal(r.Serial()),
		RevokedAt: r.RevokedAt(),
		RevokedBy: r.RevokedBy(),
		Reason:    r.Reason(),
	}
}

// parseCertSerial normalizes a serial as an operator would paste it. Two formats
// are in the wild and nothing in a serial says which it is: audit logs print
// decimal, while openssl prints hex, sometimes with colons. Reading one as the
// other would succeed and revoke a certificate that does not exist, so digits
// alone mean decimal, as a hex serial without a letter is vanishingly unlikely
// at 128 random bits, and a 0x prefix settles any doubt. Callers show the result
// in both bases so a wrong guess is visible before it counts.
func parseCertSerial(input string) (string, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return "", fmt.Errorf("certificate serial is required")
	}

	if strings.IndexFunc(s, func(r rune) bool { return r < '0' || r > '9' }) == -1 {
		n, ok := new(big.Int).SetString(s, 10)
		if !ok {
			return "", fmt.Errorf("invalid certificate serial %q", input)
		}
		s = n.Text(16)
	} else {
		s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
		s = strings.ReplaceAll(s, ":", "")
	}

	return rpc.ParseCertSerial(s)
}

func serialDecimal(serial string) string {
	n, ok := new(big.Int).SetString(serial, 16)
	if !ok {
		return ""
	}
	return n.String()
}

func AuthRevoke(ctx *Context, opts struct {
	Serial string `position:"0" usage:"Certificate serial, in hex or decimal" required:"true"`
	Reason string `short:"r" long:"reason" description:"Why the certificate is being revoked"`
	Yes    bool   `short:"y" long:"yes" description:"Skip confirmation prompt"`
	FormatOptions
	ConfigCentric
}) error {
	serial, err := parseCertSerial(opts.Serial)
	if err != nil {
		return err
	}

	// JSON output has to stay machine-readable, so it never prompts.
	if opts.IsJSON() && !opts.Yes {
		return fmt.Errorf("--format json requires --yes")
	}
	confirmed, err := confirmDeletion(
		fmt.Sprintf("Revoke certificate %s (decimal %s)? This cannot be undone from the CLI.", serial, serialDecimal(serial)),
		opts.Yes, false, false)
	if err != nil {
		return err
	}
	if !confirmed {
		return nil
	}

	client, err := ctx.RPCClient(string(rpc.ServiceCertRevocations))
	if err != nil {
		return err
	}
	defer client.Close()

	res, err := certrevocation_v1alpha.NewCertRevocationsClient(client).Revoke(ctx, serial, opts.Reason)
	if err != nil {
		return err
	}

	if opts.IsJSON() {
		return PrintJSON(struct {
			revocationJSON
			AlreadyRevoked bool `json:"already_revoked"`
		}{newRevocationJSON(res.Revocation()), res.AlreadyRevoked()})
	}

	if res.AlreadyRevoked() {
		ctx.Printf("Certificate %s was already revoked on %s\n", serial, res.Revocation().RevokedAt())
		return nil
	}

	ctx.Completed("Revoked certificate %s (decimal %s)", serial, serialDecimal(serial))
	// The cluster has no record of what it issued, so success proves only that
	// this serial is now refused, not that it was the certificate you meant.
	fmt.Fprintln(ctx.Stderr, "Warning: the cluster keeps no record of issued certificates, so it cannot confirm this serial belongs to a real one. Check it against the certificate or audit log.")
	fmt.Fprintln(ctx.Stderr, "Warning: a runner's certificate may already have renewed itself under a new serial. If this was one, also run `miren runner delete` and check the audit log for other serials with that runner's subject.")
	return nil
}

func AuthRevocationList(ctx *Context, opts struct {
	FormatOptions
	ConfigCentric
}) error {
	client, err := ctx.RPCClient(string(rpc.ServiceCertRevocations))
	if err != nil {
		return err
	}
	defer client.Close()

	res, err := certrevocation_v1alpha.NewCertRevocationsClient(client).List(ctx)
	if err != nil {
		return err
	}
	revocations := res.Revocations()

	if opts.IsJSON() {
		items := make([]revocationJSON, 0, len(revocations))
		for _, r := range revocations {
			items = append(items, newRevocationJSON(r))
		}
		return PrintJSON(items)
	}

	if len(revocations) == 0 {
		ctx.Printf("No client certificates have been revoked\n")
		return nil
	}

	headers := []string{"SERIAL", "REVOKED", "BY", "REASON"}
	var rows []ui.Row
	for _, r := range revocations {
		rows = append(rows, ui.Row{r.Serial(), r.RevokedAt(), r.RevokedBy(), r.Reason()})
	}
	table := ui.NewTable(
		ui.WithColumns(ui.AutoSizeColumns(headers, rows, ui.Columns().NoTruncate(0))),
		ui.WithRows(rows),
	)
	ctx.Printf("%s\n", table.Render())
	return nil
}
