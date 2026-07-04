import "next-auth";
import "next-auth/jwt";

declare module "next-auth" {
  interface User {
    role?: "user" | "admin";
    username?: string;
  }

  interface Session {
    user: {
      id: string;
      username: string;
      email: string;
      name: string;
      role: "user" | "admin";
    };
  }
}

declare module "next-auth/jwt" {
  interface JWT {
    id?: string;
    username?: string;
    role?: "user" | "admin";
  }
}
