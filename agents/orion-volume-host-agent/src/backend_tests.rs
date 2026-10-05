#[cfg(test)]
mod tests {
    use crate::backend::{parse_backend, BackendType, MockBackend, VolumeBackend, VolumeSpec};

    #[test]
    fn backend_selection_is_case_insensitive() {
        assert_eq!(parse_backend("CePh"), BackendType::Ceph);
        assert_eq!(parse_backend("mock"), BackendType::Mock);
        assert_eq!(parse_backend("unknown"), BackendType::LVM);
    }

    #[tokio::test]
    async fn mock_backend_is_idempotent_and_deletable() {
        let backend = MockBackend::new();
        let spec = VolumeSpec {
            volume_id: "vol_123".to_string(),
            size_gb: 10,
            backend: BackendType::Mock,
            pool: None,
        };
        let first = backend.create(&spec).await.expect("create");
        let second = backend.create(&spec).await.expect("idempotent create");
        assert_eq!(first, second);
        assert!(backend.exists("vol_123").await.expect("exists"));
        backend.delete("vol_123").await.expect("delete");
        assert!(!backend.exists("vol_123").await.expect("deleted"));
    }
}
