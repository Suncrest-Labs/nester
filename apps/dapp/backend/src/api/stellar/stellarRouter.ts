import { OpenAPIRegistry } from "@asteasolutions/zod-to-openapi";
import express, { type Router } from "express";
import { z } from "zod";

import { createApiResponse } from "@/api-docs/openAPIResponseBuilders";
import { validateRequest } from "@/common/utils/httpHandlers";
import { stellarController } from "./stellarController";
import { ValidateBalanceRequestSchema } from "./stellarModel";

export const stellarRegistry = new OpenAPIRegistry();
export const stellarRouter: Router = express.Router();

stellarRegistry.registerPath({
  method: "post",
  path: "/stellar/validate-balance",
  tags: ["Stellar"],
  request: {
    body: {
      content: {
        "application/json": {
          schema: ValidateBalanceRequestSchema.shape.body,
        },
      },
    },
  },
  responses: createApiResponse(z.object({ all_sufficient: z.boolean() }), "Balance validation result"),
});

stellarRouter.post(
  "/validate-balance",
  validateRequest(ValidateBalanceRequestSchema),
  stellarController.validateBalance,
);
