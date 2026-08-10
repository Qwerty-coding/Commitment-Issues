// Base configuration
const APP_VERSION = "2.1.0";

<<<<<<< HEAD
// OURS: Non-colliding additions (Safe to apply directly)
const ENABLE_CACHE = true;

function calculateCacheTTL() {
    return 3600;
}

// OURS: Colliding variable update (Requires AI resolution)
const API_ENDPOINT = "https://api.v2.service.com";

// OURS: Colliding function modification (Requires AI resolution)
function authenticateUser(user) {
    console.log("Authenticating via OAuth2 token...");
    return user.token != null;
}
=======
// THEIRS: Non-colliding additions (Safe to apply directly)
const ENABLE_LOGGING = true;

function getLogLevel() {
    return "DEBUG";
}

// THEIRS: Colliding variable update (Requires AI resolution)
const API_ENDPOINT = "https://graphql.service.com";

// THEIRS: Colliding function modification (Requires AI resolution)
function authenticateUser(user) {
    console.log("Authenticating via JWT bearer...");
    return user.jwt != null;
}
>>>>>>> feature-branch
