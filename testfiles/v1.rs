struct User {
    username: String,
    email: String,
}

impl User {
    fn new(username: String, email: String) -> User {
        User {
            username,
            email,
        }
    }
}