# Configuração do Cloudflare R2

Este repositório não automatiza recursos de conta da Cloudflare. Eles devem ser
criados uma única vez no painel, com uma credencial limitada ao bucket.

## Bucket e credencial

1. Crie um bucket dedicado apenas ao snapshot público.
2. Crie uma credencial de API com permissão **Object Read & Write** somente
   nesse bucket.
3. Armazene o Access Key ID e o Secret Access Key no gerenciador de segredos,
   usando as chaves `R2_ACCESS_KEY_ID` e `R2_SECRET_ACCESS_KEY`.
4. Preencha o account ID e o nome do bucket apenas no overlay privado.

Não use um token com escopo de conta. O produtor faz somente um `PUT` no objeto
`snapshot.json` e não precisa administrar buckets.

## Domínio público

Conecte `api.renara.dev` pela opção de **domínio customizado do próprio bucket
R2**. Não crie um CNAME manual e não coloque o registro em modo somente DNS. O
fluxo do R2 configura o proxy e o certificado necessários para que o objeto seja
entregue pelo CDN.

Depois da propagação, confirme que o arquivo responde em:

```text
https://api.renara.dev/snapshot.json
```

O uploader envia `Cache-Control: public, max-age=3600` em toda atualização.

## CORS

Configure a política CORS do bucket com:

- origem permitida: `https://www.renara.dev`;
- método permitido: `GET`;
- header permitido: `Content-Type`;
- idade máxima: `3600` segundos.

Não adicione `*` como origem. Caso a origem real do portfólio mude, atualize a
política explicitamente.

## Verificações finais

```bash
curl -fsSI https://api.renara.dev/snapshot.json
curl -fsS https://api.renara.dev/snapshot.json
curl -fsSI \
  -H 'Origin: https://www.renara.dev' \
  https://api.renara.dev/snapshot.json
```

Verifique o `Cache-Control`, o `Content-Type` e o
`Access-Control-Allow-Origin`. Abra também o corpo e confirme que ele contém
somente os campos documentados no README.

