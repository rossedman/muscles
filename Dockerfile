# The release image, built by goreleaser (dockers_v2 in .goreleaser.yaml)
# from the binaries it already built, for CI systems that run steps in
# containers:
#
#   docker run --rm -v "$PWD:/work" -w /work ghcr.io/rossedman/muscles images check app.tar
#
# distroless/static has CA certificates for registries, and nothing else.
FROM gcr.io/distroless/static-debian12:nonroot
ARG TARGETPLATFORM
COPY $TARGETPLATFORM/muscles /usr/bin/muscles
ENTRYPOINT ["/usr/bin/muscles"]
