fn main() -> Result<(), Box<dyn std::error::Error>> {
    let manifest_dir = std::path::Path::new(env!("CARGO_MANIFEST_DIR"));
    let proto_dir = manifest_dir.join("proto");
    let root_proto_dir = manifest_dir.join("../../proto");
    let out_dir = manifest_dir.join("src/generated");

    std::fs::create_dir_all(&out_dir)?;

    tonic_build::configure()
        .build_server(true)
        .build_client(true)
        .out_dir(out_dir.to_str().unwrap())
        .compile_protos(
            &[
                proto_dir.join("common/v1/common.proto"),
                proto_dir.join("node/v1/node.proto"),
                proto_dir.join("node/v1/node_service.proto"),
                root_proto_dir.join("placement/v1/placement.proto"),
            ],
            &[proto_dir.as_path(), root_proto_dir.as_path()],
        )?;

    println!("cargo:rerun-if-changed=proto/");
    Ok(())
}
