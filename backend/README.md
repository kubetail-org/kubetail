# Kubetail Backend

Go workspace that contains the modules used by the Kubetail backend

## Overview

The Kubetail application's backend consists of a Go-based backend server (`kubetail-server`) and a set of small Go-based backend agents (`kubetail-agent`) that run on each node in your cluster. The backend code is organized as a Go workspace that contains the Go modules for the server, the agent and some shared libraries:

* [server](server) - Backend server
* [agent](agent) - Backend agent
* [common](common) - Shared libraries

Please view the README in each module directory for more details.
