# Copyright 2024 The Kubetail Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# -----------------------------------------------------------

FROM node:26.13.0-alpine3.24@sha256:8996bb5821d62a8fd9bb27c1cb07462f3fdce6e47d640c6a881ead0b064ce216 AS frontend-builder
WORKDIR /apps/web

# install the pnpm that "packageManager" pins; corepack cannot run pnpm 12's native binary
COPY apps/web/package.json ./
RUN npm install -g "$(node -p 'require("./package.json").packageManager.split("+")[0]')"

# fetch dependencies
COPY apps/web/pnpm-lock.yaml ./
COPY apps/web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile

# copy code
COPY apps/web/ .

# build
RUN pnpm build

ENTRYPOINT []
CMD []

# -----------------------------------------------------------

FROM golang:1.26.11@sha256:7af425da11f4fc1a9308eb704369e3f6d1c44e35bac0f874a59ade3f20fdc07e AS dashboard-builder

WORKDIR /work

# install dependencies (for cache)
COPY libs/go-common/go.mod ./libs/go-common/go.mod
COPY libs/go-common/go.sum ./libs/go-common/go.sum
COPY dashboard/go.mod ./dashboard/go.mod
COPY dashboard/go.sum ./dashboard/go.sum
RUN cd dashboard && go mod download all

# copy code
COPY libs/go-common ./libs/go-common
COPY dashboard ./dashboard

# copy frontend
COPY --from=frontend-builder /apps/web/dist ./dashboard/website

# build dashboard
RUN cd dashboard && CGO_ENABLED=0 go build -ldflags="-s -w" -o /work/bin/dashboard ./cmd/main.go

ENTRYPOINT ["./bin/dashboard"]
CMD []

# -----------------------------------------------------------

FROM scratch AS final

WORKDIR /dashboard

# copy certs for tls verification
COPY --from=dashboard-builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# copy dashboard
COPY --from=dashboard-builder /work/bin/dashboard /dashboard/dashboard

ENTRYPOINT ["./dashboard"]
CMD []

# -----------------------------------------------------------

FROM alpine:3.24.4@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf AS final-alpine

WORKDIR /dashboard

# copy certs for tls verification
COPY --from=dashboard-builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# copy dashboard
COPY --from=dashboard-builder /work/bin/dashboard /dashboard/dashboard

ENTRYPOINT ["./dashboard"]
CMD []
