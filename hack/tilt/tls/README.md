# Dev TLS

## CA commands

1. Generate the CA private key:

    ```console
    openssl genpkey \
      -algorithm RSA \
      -out ca.key \
      -pkeyopt rsa_keygen_bits:4096
    ```

2. Create the self-signed CA cert (10 years valid)

    ```console
    openssl req -x509 \
      -new -nodes \
      -key ca.key \
      -config csr.cnf \
      -days 3650 \
      -out ca.crt
    ```

## Server commands

1. Generate the server private key

    ```console
    openssl genpkey \
      -algorithm RSA \
      -out server.key \
      -pkeyopt rsa_keygen_bits:4096
    ```

2. Create the CSR

    ```console
    openssl req -new \
      -key server.key \
      -out server.csr \
      -config server.cnf
    ```

3. Sign the CSR with your CA

    ```console
    openssl x509 -req \
      -in server.csr \
      -CA ca.crt \
      -CAkey ca.key \
      -CAcreateserial \
      -out server.crt \
      -days 365 \
      -extensions v3_req \
      -extfile server.cnf
    ```
