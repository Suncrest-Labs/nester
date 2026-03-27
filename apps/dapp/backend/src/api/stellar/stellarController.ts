import type { Request, Response } from "express";

import { handleServiceResponse } from "@/common/utils/httpHandlers";
import { validateBalances } from "./stellarService";

class StellarController {
  async validateBalance(req: Request, res: Response) {
    const { account_id, payments } = req.body;
    const serviceResponse = await validateBalances(account_id, payments);
    return handleServiceResponse(serviceResponse, res);
  }
}

export const stellarController = new StellarController();
