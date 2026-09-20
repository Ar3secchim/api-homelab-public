# ADR 0002: allowlist no produtor e privilégio mínimo

## Status

Aceita.

## Contexto

Filtrar dados apenas no frontend não impede que o arquivo original seja lido
diretamente. Uma denylist também falha quando surge um novo tipo de dado que não
foi antecipado.

## Decisão

O produtor monta um objeto novo, campo a campo. Apenas contagens, estados
agregados, timestamps e uma lista fixa de serviços podem entrar no contrato.
Depois da montagem, o JSON completo é validado novamente antes do upload.

A ServiceAccount pode executar somente `get` e `list` sobre:

- Namespaces e Pods;
- Applications do Argo CD;
- Certificates do cert-manager.

Não há curinga e não existe acesso a Secrets ou ConfigMaps. O total de
workloads, sua distribuição por tipo e a quantidade de nós observados são
inferidos dos Pods para não ampliar essas permissões.

## Consequências

- mudanças no contrato exigem alteração consciente no código e nos testes;
- nomes descobertos nunca alcançam o arquivo público;
- mesmo um bug no serializador não consegue ler Secrets com essa identidade;
- o indicador de workloads representa controladores observados, e não todos os
  objetos declarados que estejam sem Pods.
