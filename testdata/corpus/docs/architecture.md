# Architecture Design

## Overview

The system architecture combines fast lexical indexing with semantic embeddings using a Hybrid Search pipeline.

```
       +--------------------+
       |   User Query       |
       +---------+----------+
                 |
        +--------+--------+
        |                 |
  +-----v------+    +-----v------+
  |    BM25    |    |   Vector   |
  |  Lexical   |    |  Semantic  |
  +-----+------+    +-----+------+
        |                 |
        +--------+--------+
                 |
           +-----v------+
           | RRF Fusion |
           +------------+
```

## Modular Layers

1. **Routing & Fast Path**: Direct keyword and file path matching bypasses LLM inference for common patterns.
2. **Hybrid Resolution**: Combines BM25 term weighting and vector dot product similarities using Reciprocal Rank Fusion (RRF).
3. **Adaptive Oversampling**: Compensates for directory scoping by oversampling candidates before filtering.
4. **Safety Confinement**: Restricts reads strictly within allowed workspace boundaries.
