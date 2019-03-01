//
// Created by tang zhige on 2019/3/1.
//

#ifndef ETHASH_STUB_H
#define ETHASH_STUB_H

#include "stdio.h"
#include "mmap.h"

inline void* stub_calloc(size_t n, size_t size, const char* name) {
    printf("STUB CALLOC: %s", name);
    return calloc(n,size);
}

inline void* stub_malloc(size_t size, const char* name) {
    printf("STUB MALLOC: %s", name);
    return malloc(size,name);
}

inline void stub_free(void* ptr,const char* name) {
    printf("STUB FREE: %s", name);
    return free(ptr);
}

inline void* stub_mmap(void* start, size_t length, int prot, int flags, int fd, off_t offset,const char* name) {
    printf("STUB MMAP: %s", name);
    return mmap(start,length,prot,flags,fd,offset);
}

inline void stub_munmap(void* addr, size_t length,const char* name) {
    printf("STUB NMAP: %s", name);
    munmap(addr,length);
}

#endif //ETHASH_STUB_H
