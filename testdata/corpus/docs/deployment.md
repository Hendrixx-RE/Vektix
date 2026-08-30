# Kubernetes Deployment

## Overview

We deploy microservices using Helm charts to Kubernetes clusters.

## HorizontalPodAutoscaler

Autoscaling configuration is defined in `deploy/hpa.yaml`:

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: api-server-hpa
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: api-server
  minReplicas: 3
  maxReplicas: 20
  metrics:
  - type: Resource
    resource:
      name: cpu
      target:
        type: Utilization
        averageUtilization: 75
```

## Resource Requests and Limits

Each pod is allocated 500m CPU and 512Mi memory with a burst limit of 2 CPU and 2Gi memory.
