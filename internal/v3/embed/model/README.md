# all-MiniLM-L6-v2 ONNX

The M1 build needs the ONNX export of `sentence-transformers/all-MiniLM-L6-v2`
and its tokenizer placed here at build time. These files are NOT checked into
git (size ~90 MB) — CI downloads them on cold cache, developers download
once locally.

## Expected files

- `model.onnx`       (~90 MB)
- `tokenizer.json`   (~1 MB)

## Download

```bash
mkdir -p internal/v3/embed/model
curl -L -o internal/v3/embed/model/model.onnx \
  https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main/onnx/model.onnx
curl -L -o internal/v3/embed/model/tokenizer.json \
  https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main/tokenizer.json
```

Or use: `make v3-fetch-model`
