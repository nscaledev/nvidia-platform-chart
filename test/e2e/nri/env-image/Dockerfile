# A workload image that sets NCCL_IB_PKEY itself (in ENV, not the pod spec).
ARG BASE=busybox:1.37.0
FROM ${BASE}
ENV NCCL_IB_PKEY=5
