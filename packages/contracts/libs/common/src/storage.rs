use soroban_sdk::{symbol_short, Symbol};

pub fn admin_key() -> Symbol {
    symbol_short!("admin")
}

pub fn balance_key(_account: &Symbol) -> Symbol {
    symbol_short!("bal")
}

pub fn strategy_key(_id: &u32) -> Symbol {
    symbol_short!("strat")
}

pub fn role_key(_account: &Symbol, _role: &Symbol) -> Symbol {
    symbol_short!("role")
}

pub fn initialized_key() -> Symbol {
    symbol_short!("init")
}

pub fn reentrancy_lock_key() -> Symbol {
    symbol_short!("RE_LOCK")
}
