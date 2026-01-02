{{- /*gotype:github.com/moru-ai/sandbox-infra/packages/orchestrator/internal/template/build/core/rootfs.templateModel*/ -}}
{{ .WriteFile "/etc/hostname" 0o644 }}

{{ .Hostname }}