package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/jerkytreats/dns-operator/internal/migration"
)

func main() {
	var (
		namespace = flag.String(
			"namespace",
			migration.DefaultNamespace,
			"Namespace for imported resources",
		)
		bundleName = flag.String(
			"bundle-name",
			migration.DefaultBundleName,
			"Name for the imported shared CertificateBundle",
		)
		nameserverAddress = flag.String(
			"nameserver-address",
			"",
			"Current restricted nameserver address from Tailscale admin state",
		)
		configPath             = flag.String("config", "", "Path to exported config.yaml")
		zoneFilePath           = flag.String("zone-file", "", "Path to exported authoritative zone file")
		proxyRulesPath         = flag.String("proxy-rules", "", "Path to exported proxy_rules.json")
		certificateDomainsPath = flag.String(
			"certificate-domains",
			"",
			"Path to exported certificate_domains.json",
		)
		caddyfilePath = flag.String("caddyfile", "", "Optional path to exported rendered Caddyfile")
		outputPath    = flag.String(
			"output",
			"",
			"Path to write imported resources YAML, defaults to stdout",
		)
		reportPath = flag.String("report", "", "Optional path to write the import report JSON")
	)
	flag.Parse()

	input := migration.ImportInput{
		Namespace:         *namespace,
		BundleName:        *bundleName,
		NameserverAddress: *nameserverAddress,
	}

	var err error
	if input.ConfigYAML, err = readFile(*configPath); err != nil {
		exitf("read config: %v", err)
	}
	if input.ZoneFile, err = readFile(*zoneFilePath); err != nil {
		exitf("read zone file: %v", err)
	}
	if input.ProxyRulesJSON, err = readFile(*proxyRulesPath); err != nil {
		exitf("read proxy rules: %v", err)
	}
	if input.CertificateDomainsJSON, err = readFile(*certificateDomainsPath); err != nil {
		exitf("read certificate domains: %v", err)
	}
	if input.Caddyfile, err = readFile(*caddyfilePath); err != nil {
		exitf("read caddyfile: %v", err)
	}

	result, err := migration.Import(input)
	if err != nil {
		exitf("import reference data: %v", err)
	}

	renderedYAML, err := migration.RenderYAML(result.Objects)
	if err != nil {
		exitf("render resources yaml: %v", err)
	}
	if err := writeOutput(*outputPath, renderedYAML); err != nil {
		exitf("write resources yaml: %v", err)
	}

	if *reportPath != "" {
		reportData, err := migration.RenderReport(result.Report)
		if err != nil {
			exitf("render import report: %v", err)
		}
		if err := os.WriteFile(*reportPath, reportData, 0o644); err != nil {
			exitf("write import report: %v", err)
		}
	}
}

func readFile(path string) ([]byte, error) {
	if path == "" {
		return nil, nil
	}
	return os.ReadFile(path)
}

func writeOutput(path string, data []byte) error {
	if path == "" {
		_, err := os.Stdout.Write(data)
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func exitf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
