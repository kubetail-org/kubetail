# Kubetail Backend

Go workspace that contains the modules used by the Kubetail backend

## Overview

The Kubetail application's backend consists of a Go-based backend server and a set of small Go-based backend agents that run on each node in your cluster. The backend code is organized as a Go workspace that contains the Go modules for the server, the agent and shared libraries:

* [server](server) - Backend server
* [agent](agent) - Backend agent
* [common](common) - Shared libraries

This workspace also contains the Protocol Buffer definition files for the agent:

* [proto](proto) - Protocol Buffer definition files 

Please view the README in each directory for more details. 
