//
// Created by tang zhige on 2019/3/1.
//

#ifndef ETHASH_STUB_H
#define ETHASH_STUB_H

void* stub_calloc(size_t n, size_t size, const char* name);

void* stub_malloc(size_t size, const char* name);

void stub_free(void* ptr,const char* name);

void* stub_mmap(void* start, size_t length, int prot, int flags, int fd, off_t offset,const char* name);

void stub_munmap(void* addr, size_t length,const char* name);

#endif //ETHASH_STUB_H
