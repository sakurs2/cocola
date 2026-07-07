declare module "swagger-ui-react" {
  import type { ComponentType } from "react";

  export type SwaggerUIProps = {
    url?: string;
    spec?: unknown;
    deepLinking?: boolean;
    displayOperationId?: boolean;
    displayRequestDuration?: boolean;
    docExpansion?: "list" | "full" | "none";
    defaultModelsExpandDepth?: number;
    tryItOutEnabled?: boolean;
    persistAuthorization?: boolean;
    supportedSubmitMethods?: string[];
  };

  const SwaggerUI: ComponentType<SwaggerUIProps>;
  export default SwaggerUI;
}
