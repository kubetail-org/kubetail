# Copyright 2024 Andres Morey
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

FROM golang:1.21.6 AS backend-builder

RUN mkdir backend
WORKDIR /backend

# install dependencies (for cache)
COPY backend/go.mod .
COPY backend/go.sum .
RUN go mod download

# copy code
COPY backend/ .

# build server
RUN CGO_ENABLED=0 go build -o server ./cmd/server

ENTRYPOINT ["./server"]
CMD []

# -----------------------------------------------------------

FROM node:20.11.0-alpine3.18 AS frontend-builder

RUN mkdir frontend
WORKDIR /frontend

# enable pnpm
RUN corepack enable
RUN corepack prepare pnpm@8.15.5 --activate

# set up git+ssh for private package download from github
RUN apk add git openssh-client
RUN mkdir -p -m 0700 ~/.ssh && ssh-keyscan github.com >> ~/.ssh/known_hosts

# fetch dependencies
COPY frontend/package.json ./
COPY frontend/pnpm-lock.yaml ./
RUN --mount=type=ssh pnpm install

# copy code
COPY frontend/ .

# build
RUN pnpm build

ENTRYPOINT []
CMD []

# -----------------------------------------------------------

FROM alpine:3.19.1 AS debug

# copy certs for tls verification
COPY --from=backend-builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# copy backend
COPY --from=backend-builder /backend/server /app/server
COPY --from=backend-builder /backend/templates /app/templates


# copy frontend
COPY --from=frontend-builder /frontend/dist /app/website

WORKDIR /app

ENTRYPOINT ["./server"]
CMD []

# -----------------------------------------------------------

FROM scratch AS final

# copy certs for tls verification
COPY --from=backend-builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# copy backend
COPY --from=backend-builder /backend/server /app/server
COPY --from=backend-builder /backend/templates /app/templates

# copy frontend
COPY --from=frontend-builder /frontend/dist /app/website

WORKDIR /app

ENTRYPOINT ["./server"]
CMD []
