# LSVD - Log Structured Virtual Disk

### Status: Alpha

lsvd provides an implementation of [LSVD](), which is an abstraction on top of object storage
systems to provide block device semantics and speed.

This is done by storing data in chunks as the data is written, and caching those chunks locally
as well as uploading them to an object storage system.

This implementation is very faithful to the original paper, but adds at last one additional
feature: compression. As data is added a chunk, it is first compressed with LZ4. When the data
is then read from the cache, it is uncompressed before being returned. Because of the way most
OSes cache block device data, this has virtually zero impact negative impact on read speed.
In fact because the data is compressed, in the case of cache misses, the read speed is improved
as there is less data to download from the object storage system.

## Usage

The virtual disk is exposed as a Network Block Device (nbd). As such, once the lsvd program is running
the OS tools to mount it are required. Here is a brief session of creating a disk and mounting
it on linux:

```bash

# Create a config file
cat <<DONE > lsvd.hcl
cache_path = "./data/cache"

storage {
  s3 {
    bucket = "lsvdinttest"
    region = "us-east-1"
    access_key = "admin"
    secret_key = "password"
    host = "http://localhost:9000"
  }
}
DONE

# Create a 2 Terabyte disk named test.
$ lsvd volume init -c lsvd.hcl -n test -S 2T

# Start the disk
$ lsvd nbd -c lsvd.hcl -n test -p ./data/cache -a localhost:8989 &

# Attach it to a nbd
$ nbd-client lsvd localhost 8989 nbd0 -b 4096 -name test

# Create a filesystem on it
$ mkfs.ext4 /dev/nbd0

# Mount it
$ mount /dev/nbd0 /mnt/lsvd

```
