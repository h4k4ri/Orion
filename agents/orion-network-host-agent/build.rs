fn main() -> Result<(), Box<dyn std::error::Error>> {
    let root = std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join("../../proto");
    let out = std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join("src/generated");
    std::fs::create_dir_all(&out)?;
    tonic_build::configure()
        .build_server(true)
        .build_client(false)
        .out_dir(out)
        .compile_protos(&[root.join("network/v1/host_agent.proto")], &[root])?;
    println!("cargo:rerun-if-changed=../../proto/network/v1/host_agent.proto");
    Ok(())
}
