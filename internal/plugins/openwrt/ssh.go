package openwrt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"

	"netscope/internal/plugin"
	"netscope/internal/sshx"
)

// maxOutput caps the output of the remote commands.
const maxOutput = 8 << 20

// fetchSSH reads "uci show dhcp" and the dnsmasq lease files over SSH (read-only).
func fetchSSH(ctx context.Context, log *slog.Logger, host string, cred *plugin.Credential, opt sshx.Options) ([]lease, []uciSection, error) {
	cl, err := sshx.Dial(ctx, host, cred, opt)
	if err != nil {
		if errors.Is(err, sshx.ErrHostKeyChanged) {
			return nil, nil, err
		}
		return nil, nil, fmt.Errorf("SSH-Verbindung zu %s fehlgeschlagen: %w", host, err)
	}
	defer cl.Close()

	var sections []uciSection
	res, err := cl.Run(ctx, "uci show dhcp", maxOutput)
	switch {
	case err != nil:
		return nil, nil, fmt.Errorf("uci show dhcp: %w", err)
	case res.ExitCode != 0:
		log.Warn("uci show dhcp fehlgeschlagen, statische Leases fehlen", "exit", res.ExitCode,
			"stderr", string(bytes.TrimSpace(res.Stderr)))
	default:
		sections = parseUCIShow(res.Stdout)
	}

	var (
		leases []lease
		read   int
	)
	for _, file := range leaseFiles(sections) {
		data, err := cl.ReadAll(ctx, file, maxOutput)
		if err != nil {
			if ctx.Err() != nil {
				return nil, nil, ctx.Err()
			}
			log.Warn("Lease-Datei nicht lesbar", "file", file, "error", err)
			continue
		}
		read++
		leases = append(leases, parseLeases(data)...)
	}
	if read == 0 && sections == nil {
		return nil, nil, errors.New("weder Lease-Datei noch DHCP-Konfiguration lesbar – ist das Ziel ein OpenWrt-Router?")
	}
	return leases, sections, nil
}
