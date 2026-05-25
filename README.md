<p align="center">
  <img src="repo-assets/go-fucking-lang.png" alt="Go Image Extractor Logo" width="300">
</p>

# Extrator de Imagens (Go)

Uma ferramenta de linha de comando de alta performance escrita em Go para extrair links de imagens de planilhas Excel (`.xlsx`) ou CSV (`.csv`), baixá-las em lote concorrentemente e convertê-las para Base64.

Esta ferramenta roda inteiramente em memória e **não altera a planilha original**.

---

## O que a ferramenta faz?

1. **Escaneamento**: Varre todas as linhas da planilha identificando células que contêm links de imagem.
2. **Download Concorrente**: Baixa os arquivos das imagens em paralelo utilizando múltiplos workers para acelerar o processo.
3. **Resolução Máxima (@4x)**: Tenta baixar automaticamente a versão de alta resolução `@4x` (ex: `imagem@4x.png`). Se não existir no servidor, faz o fallback automático para a versão original `@1x`.
4. **Cache de Duplicados**: Garante que links idênticos (como logotipos repetidos) sejam baixados apenas uma vez, poupando rede e tempo.
5. **Base64**: Converte cada imagem em uma string Base64 Data URI.

---

## Estrutura de Retorno

Todos os arquivos são gravados na pasta de destino informada (padrão: `./output_images/`):

```text
output_images/
├── manifest.json      # Mapeamento geral (URL original -> Base64 e caminhos locais)
├── base64/            # Arquivos de texto (.txt) contendo apenas a string Base64 de cada imagem
└── binary/            # Arquivos de imagem originais (.png, .jpg, etc.)
```

Os arquivos de saída são nomeados usando o identificador da linha (ex: coluna `name` ou `ID da Recompensa`) combinado com o nome da coluna de origem (ex: `photo_href`).

Exemplo do `manifest.json`:
```json
{
  "https://assets.bonuz.com/prizes/archie-projeto-completo/prize-photo.png": {
    "base64_data": "data:image/png;base64,iVBORw0KGgoAAAANS...",
    "mime_type": "image/png",
    "binary_file": "binary/archie-projeto-completo_photo_href.png",
    "base64_file": "base64/archie-projeto-completo_photo_href.txt",
    "actual_url": "https://assets.bonuz.com/prizes/archie-projeto-completo/prize-photo@4x.png"
  }
}
```

---

## Como Rodar e Integrar

### Pré-requisitos
- **Para rodar**: O binário compilado não exige nenhuma dependência ou instalação adicional.
- **Para compilar**: Go 1.20+ instalado.

### Como Compilar o Binário
Compile o executável de acordo com o sistema operacional de destino:

**Para macOS (Local):**
```bash
go build -o image_extractor main.go
```

**Para Linux (Servidores de automação/Docker):**
```bash
GOOS=linux GOARCH=amd64 go build -o image_extractor main.go
```

### Como Rodar (Linha de Comando)
Execute o binário passando os parâmetros desejados:

```bash
./image_extractor --input "Planilha.xlsx"
```

#### Parâmetros disponíveis (Flags):
- `--input` *(Obrigatório)*: Caminho para a planilha `.xlsx` ou `.csv` de entrada.
- `--output` *(Padrão: `output_images`)*: Pasta onde serão salvos os arquivos extraídos.
- `--column` *(Padrão: `photo/href`)*: Filtra apenas as colunas que contêm esse texto no cabeçalho (use `""` para processar todas as colunas).
- `--scale` *(Padrão: `4x`)*: Escala de resolução das imagens (`2x`, `3x`, `4x` ou `1x` para original).
- `--workers` *(Padrão: `20`)*: Quantidade de downloads simultâneos.

---

## Integração em Automações

### Exemplo em Script Bash:
```bash
#!/usr/bin/env bash
set -e

# Roda o extrator buscando apenas a coluna de foto
./image_extractor --input "planilha_mensal.xlsx" --output "./saida" --column "photo/href"

# Os arquivos e o manifest estarão disponíveis em ./saida/
```

### Exemplo em Node.js (Child Process):
```javascript
const { execFile } = require('child_process');

execFile('./image_extractor', ['--input', 'dados.xlsx'], (error, stdout) => {
  if (error) throw error;
  console.log('Extração concluída com sucesso!');
});
```
