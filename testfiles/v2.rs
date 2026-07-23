struct User {
    username: String,
    age: u8,
}

impl User {
    fn new(username: String, age: u8) -> User {
        User {
            username,
            age,
        }
    }
}