{{- /*gotype:github.com/moru-ai/sandbox-infra/packages/orchestrator/internal/template/build/core/rootfs.templateModel*/ -}}
{{ .WriteFile "/etc/resolv.conf" 0o644 }}

nameserver {{ .Nameserver }}