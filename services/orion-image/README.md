# orion-image

Servico de imagens do Orion.

Inspiracao OpenStack:

- `Glance`
- separacao entre metadados e dados da imagem
- catalogo central de imagens para boot

Escopo inicial:

- catalogo;
- upload;
- metadados;
- distribuicao de imagens para boot.

Simplificacoes deliberadas:

- store local em filesystem no primeiro corte;
- sem upload arbitrario inicialmente;
- bootstrap de uma imagem oficial pequena para provar boot real.

Tambem estao disponiveis os fluxos de criar metadados, upload com limite de
20 GiB e checksum SHA-256, download e remocao de imagens nao protegidas.
