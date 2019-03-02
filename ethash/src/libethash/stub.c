//
// Created by tang zhige on 2019/3/1.
//

#include "stub.h"
#include "stdio.h"
#include "mmap.h"

void* stub_calloc(size_t n, size_t size, const char* name) {
    printf("STUB CALLOC: %s\n", name);
    return calloc(n,size);
}

void* stub_malloc(size_t size, const char* name) {
    printf("STUB MALLOC: %s\n", name);
    return malloc(size);
}

void stub_free(void* ptr,const char* name) {
    printf("STUB FREE: %s\n", name);
    return free(ptr);
}

void* stub_mmap(void* start, size_t length, int prot, int flags, int fd, off_t offset,const char* name) {
    printf("STUB MMAP: %s\n", name);
    return mmap(start,length,prot,flags,fd,offset);
}

void stub_munmap(void* addr, size_t length,const char* name) {
    printf("STUB NMAP: %s\n", name);
    munmap(addr,length);
}
