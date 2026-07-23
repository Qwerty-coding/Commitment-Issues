struct User {
    username: String,
    email: String,
    age: u8,
}

impl User {
    fn new(username: String, email: String, age: u8) -> User {
        User {
            username,
            email,
            age,
        }
    }
}