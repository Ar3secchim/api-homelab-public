# Homelab Public API

Publicação segura de um snapshot curado de um cluster Kubernetes para uso em
portfólio. O produtor roda dentro do cluster, calcula apenas indicadores
agregados e envia um único arquivo JSON para um bucket Cloudflare R2.

O projeto não abre portas, não cria túneis e não expõe a API do Kubernetes. O
fluxo de rede é exclusivamente de saída:

```text
Kubernetes CronJob -> Cloudflare R2 -> navegador
```

## Dados publicados

O documento tem um contrato fechado:

```json
{
  "generatedAt": "2026-09-20T14:00:00Z",
  "gitops": {
    "applications": 0,
    "synced": 0,
    "healthy": 0,
    "lastSyncAt": null
  },
  "scale": {
    "namespaces": 0,
    "workloads": 0,
    "podsRunning": 0
  },
  "tls": {
    "certificates": 0,
    "daysToNextRenewal": null
  },
  "services": ["ArgoCD", "Traefik", "cert-manager", "Infisical"]
}
```

Nomes descobertos no cluster nunca são serializados. A lista `services` é fixa
no código. “Workloads” é a quantidade de controladores distintos inferida a
partir dos Pods; isso evita conceder leitura adicional sobre Deployments,
StatefulSets ou DaemonSets.

Antes do upload, o documento completo passa por duas barreiras:

1. validação exata de campos, tipos, relações entre contadores e lista de
   serviços;
2. busca por padrões proibidos, incluindo endereços privados, nomes internos e
   versões de software.

Qualquer falha termina o Job antes do envio e preserva o último snapshot válido
no bucket.

## Estrutura

```text
cmd/snapshot/           ponto de entrada do produtor
internal/               cliente Kubernetes, sanitizador e cliente R2
k8s/base/               RBAC mínimo, CronJob e integração com Infisical
docs/decisions/         decisões e limites de segurança
tests/                  contrato e invariantes automatizados
```

## Validação local

O produtor é escrito em Go e usa apenas a biblioteca padrão:

```bash
make fmt
make lint
make test
kubectl kustomize k8s/base >/tmp/homelab-snapshot.yaml
docker build -t homelab-snapshot:test .
```

O [`.editorconfig`](.editorconfig) padroniza UTF-8, LF, newline final e
indentação entre Go, Makefile, YAML, JSON, Markdown e shell. Para Go, `gofmt` é
o formatador canônico e `go vet` faz a análise estática; ambos são verificados
pela CI.

## API local para desenvolvimento

O servidor local usa somente dados fictícios, gera um timestamp atual a cada
requisição e aceita conexões apenas da própria máquina:

```bash
make dev
```

Endpoints:

- `http://127.0.0.1:8080/snapshot.json` — contrato usado pelo frontend;
- `http://127.0.0.1:8080/healthz` — verificação simples de disponibilidade.

Por padrão, o CORS permite `http://localhost:5173`. Para usar outra origem
local:

```bash
go run ./cmd/devserver -cors-origin http://localhost:4200
```

Esse servidor não acessa Kubernetes, Infisical ou Cloudflare R2 e não faz parte
da imagem executada pelo CronJob.

O workflow de CI usa exclusivamente runners hospedados pelo GitHub. Em pushes
na branch `main`, ele também publica a imagem no GitHub Container Registry.

## Implantação

Os manifests públicos são uma base sanitizada, não um manifesto pronto para ser
aplicado sem revisão. Em um overlay privado:

1. substitua `replace-with-account-id` e `replace-with-bucket-name` no CronJob;
2. substitua o endpoint, projeto, ambiente e caminho de exemplo no
   `InfisicalSecret`;
3. crie `infisical-operator-credentials` no namespace `portfolio` pelo processo
   privado de bootstrap do operator;
4. disponibilize `R2_ACCESS_KEY_ID` e `R2_SECRET_ACCESS_KEY` no caminho do
   Infisical configurado;
5. torne o pacote do GHCR público ou configure um `imagePullSecret` no overlay;
6. fixe a imagem por digest após o primeiro build;
7. renderize e revise o overlay antes de adicioná-lo ao GitOps.

O Secret de credenciais do operator precisa existir no mesmo namespace do
`InfisicalSecret`. Nenhum valor de credencial deve entrar neste repositório.

A configuração do R2 e o checklist de publicação estão em
[docs/cloudflare.md](docs/cloudflare.md). As decisões de segurança estão em
[docs/decisions/0001-outbound-snapshot.md](docs/decisions/0001-outbound-snapshot.md).

## Consumo

O frontend consulta `https://api.renara.dev/snapshot.json`. Ele deve comparar
`generatedAt` com o relógio atual e sinalizar explicitamente o dado como
desatualizado quando a idade ultrapassar três horas. Essa indicação é apenas de
apresentação: toda curadoria já ocorreu antes do upload.
