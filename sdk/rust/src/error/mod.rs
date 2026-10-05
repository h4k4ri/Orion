use std::collections::HashMap;

#[derive(Clone, Debug)]
pub struct OrionError {
    pub code: String,
    pub message: String,
    pub details: HashMap<String, String>,
}

impl OrionError {
    pub fn new(code: impl Into<String>, message: impl Into<String>) -> Self {
        Self {
            code: code.into(),
            message: message.into(),
            details: HashMap::new(),
        }
    }
}

impl std::fmt::Display for OrionError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}: {}", self.code, self.message)
    }
}

impl std::error::Error for OrionError {}
