# ==============================================================================
# WARNING: etcd-synthetic-load intentionally STRESSES ETCD.
#
# This image builds a tool that creates large volumes of Secrets, ConfigMaps,
# and Namespaces on a Kubernetes/OpenShift cluster in order to load-test
# etcd. It is NOT FOR USE ON ANY CLUSTER THAT IS IMPORTANT.
#
# LAB / TEST / DEV CLUSTERS ONLY.
# ==============================================================================

FROM registry.access.redhat.com/ubi9/go-toolset:latest AS build
WORKDIR /build
USER 0

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 go build -o /build/etcd-synthetic-load ./cmd/etcd-synthetic-load

# ------------------------------------------------------------------------------
# Runtime image. Includes the `oc` CLI so OC_SERVER/OC_USER/OC_PASSWORD login
# works even when no kubeconfig is mounted.
# ------------------------------------------------------------------------------
FROM registry.access.redhat.com/ubi9/ubi-minimal:latest

# WARNING: NOT FOR USE ON ANY CLUSTER THAT IS IMPORTANT. See README.md.
LABEL org.opencontainers.image.title="etcd-synthetic-load" \
      org.opencontainers.image.description="Synthetic etcd load generator - LAB/TEST/DEV CLUSTERS ONLY, not for use on any cluster that is important" \
      org.opencontainers.image.source="https://github.com/dasmlab/etcd-synthetic-load" \
      io.dasmlab.warning="NOT_FOR_USE_ON_ANY_CLUSTER_THAT_IS_IMPORTANT"

ARG OC_CLI_URL=https://mirror.openshift.com/pub/openshift-v4/clients/ocp/stable/openshift-client-linux.tar.gz

RUN microdnf install -y tar gzip ca-certificates && \
    curl -fsSL "${OC_CLI_URL}" -o /tmp/oc.tar.gz && \
    tar -xzf /tmp/oc.tar.gz -C /usr/local/bin oc && \
    rm -f /tmp/oc.tar.gz && \
    microdnf clean all

COPY --from=build /build/etcd-synthetic-load /usr/local/bin/etcd-synthetic-load

RUN useradd -u 1001 -m -s /sbin/nologin synthload
USER 1001
WORKDIR /home/synthload

ENTRYPOINT ["/usr/local/bin/etcd-synthetic-load"]
CMD ["--help"]
