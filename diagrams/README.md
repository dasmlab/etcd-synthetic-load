# Diagrams

D2 → SVG workflow diagrams for **etcd-synthetic-load**.

| Diagram | Purpose |
|---------|---------|
| [workflow](workflow.svg) | Target lifecycle: create → configure → generate → load → test → report → clean/delete |

```bash
d2 diagrams/workflow.d2 diagrams/workflow.svg
```

CI: `.github/workflows/d2-diagrams.yml`
