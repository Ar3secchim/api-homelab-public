# ADR 0001: snapshot exclusivamente de saída

## Status

Aceita.

## Contexto

Um portfólio precisa demonstrar a operação real de uma infraestrutura sem
transformar a rede doméstica em uma origem pública. Uma API servida pelo cluster
exigiria um caminho de entrada e ampliaria a superfície de ataque.

## Decisão

Um CronJob produz um arquivo estático por atualização e faz upload para um
bucket dedicado. Visitantes acessam o CDN, nunca o cluster. A cadência é horária
e a versão inicial mantém somente o estado atual.

O frontend considera o snapshot desatualizado após três horas. Uma falha de
coleta ou sanitização não remove o último arquivo válido.

## Consequências

- não há porta, túnel, DNS dinâmico ou load balancer público no ambiente;
- a indisponibilidade do cluster não impede a entrega do último snapshot;
- o estado não é em tempo real;
- a idade do snapshot revela indiretamente períodos de indisponibilidade, risco
  aceito porque não existe caminho de entrada.
