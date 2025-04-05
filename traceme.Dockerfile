FROM ubuntu:24.04 AS rr_build

RUN apt-get update && apt-get install -y \
    cmake ninja-build gcc g++ clang librocksdb-dev gdb lldb \
    pkg-config python3-pexpect capnproto libcapnp-dev debuginfod fd-find \
    gcc-14-plugin-dev g++ libstdc++-14-dev git \
    && rm -rf /var/lib/apt/lists/*


RUN git clone https://github.com/zyantific/zydis \
    && cd zydis \
    && git checkout 1ba75aeefae37094c7be8eba07ff81d4fe0f1f20 \
    && git submodule update --init --recursive
RUN cd zydis \
    && mkdir build \
    && cd build \
    && cmake .. -GNinja -DCMAKE_INSTALL_PREFIX=/opt/zydis -DCMAKE_BUILD_TYPE=Release -DZYDIS_BUILD_SHARED_LIB=OFF -DZYDIS_BUILD_EXAMPLES=OFF -DZYDIS_BUILD_TOOLS=OFF -DZYDIS_BUILD_MAN=OFF \
    -DZYDIS_BUILD_DOXYGEN=OFF \
    && ninja install zycore/install

RUN git clone https://github.com/sidkshatriya/rr.soft -b software-counters-mode \
    && cd rr.soft \
    && git checkout 24f17e3561712ed2f30d322d328f573070e0623a
RUN cd rr.soft/compiler-plugins/SoftwareCountersClangPlugin \
    && mkdir /opt/traceme \
    && make install INSTALL_PATH=/opt/traceme/

RUN cd rr.soft \
    && sed -i 's/RocksDB::rocksdb-shared/RocksDB::rocksdb/g' CMakeLists.txt \
    && sed -i 's/Snappy::snappy/Snappy::snappy-static/g' /usr/lib/x86_64-linux-gnu/cmake/rocksdb/RocksDBTargets.cmake \
    && sed -i 's/gflags::gflags_shared/gflags::gflags_static/g' /usr/lib/x86_64-linux-gnu/cmake/rocksdb/RocksDBTargets.cmake \
    && mkdir build \
    && cd build \
    && cmake .. -GNinja -DCMAKE_INSTALL_PREFIX=/opt/traceme -DCMAKE_BUILD_TYPE=Release -Dstaticlibs=ON \
    -DSOFTWARE_COUNTERS_PLUGIN=/opt/traceme/libSoftwareCounters.so -DZydis_DIR=/opt/zydis/lib/cmake/zydis -DZycore_DIR=/opt/zydis/lib/cmake/zycore \
    && ninja install

FROM ubuntu:24.04

RUN apt-get update && apt-get install -y zstd && rm -rf /var/lib/apt/lists/*
ADD build/traceme /opt/traceme/bin/traceme
RUN chmod +x /opt/traceme/bin/traceme

COPY --from=rr_build /opt/traceme /opt/traceme
