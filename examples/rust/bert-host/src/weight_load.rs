use candle_core::{Device, Result, Tensor};
use std::fs::File;
use candle_core::safetensors;
use memmap2::Mmap;
use candle_nn::{Linear, Module};
// use hf_hub::api::sync::Api;
// use candle_core::Device;


struct Model {
    first: Linear,
    second: Linear,
}

impl Model {
    fn forward(&self, image: &Tensor) -> Result<Tensor> {
        let x = self.first.forward(image)?;
        let x = x.relu()?;
        self.second.forward(&x)
    }
}

fn main() -> Result<()> {
    // Use Device::new_cuda(0)?; to use the GPU.

    
    // let device = Device::Cpu;

    // This has changed (784, 100) -> (100, 784) !
    // let weight = Tensor::randn(0f32, 1.0, (100, 784), &device)?;
    // let bias = Tensor::randn(0f32, 1.0, (100, ), &device)?;
    // let first = Linear::new(weight, Some(bias));
    // let weight = Tensor::randn(0f32, 1.0, (10, 100), &device)?;
    // let bias = Tensor::randn(0f32, 1.0, (10, ), &device)?;
    // let second = Linear::new(weight, Some(bias));
    // let model = Model { first, second };

    // let dummy_image = Tensor::randn(0f32, 1.0, (1, 784), &device)?;

    // let digit = model.forward(&dummy_image)?;
    // println!("Digit {digit:?} digit");
    // let api = Api::new().unwrap();
    // let repo = api.model("bert-base-uncased".to_string());

    // let weights = repo.get("model.safetensors").unwrap();
    let weights_filename = "model.safetensors";
    let file = File::open(weights_filename).unwrap();
    let mmap = unsafe { Mmap::map(&file).unwrap() };
    let weights = candle_core::safetensors::load_buffer(&mmap[..], &Device::Cpu).unwrap();
    println!("Weights: {:?}", weights);
    Ok(())
}