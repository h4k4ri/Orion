fn main() {
    println!("cargo:rerun-if-changed=../../proto/plugin/v1/plugin.proto");

    tonic_build::configure()
        .build_client(true)
        .build_server(true)
        .compile_protos(&["../../proto/plugin/v1/plugin.proto"], &["../../proto"])
        .expect("failed to compile Orion plugin protobufs");
}
